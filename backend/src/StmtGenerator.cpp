#include "StmtGenerator.h"

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt) {
  if (stmt.is_null()) return;

  std::string_view op = "unknown";
  if (stmt.contains("op")) {
    op = stmt["op"].get<std::string_view>();
  }

  if (op == "temp_decl") {
    llvm::Type *ty = llvmTypeFor(ctx, stmt["type"]);
    std::string name = stmt["name"].get<std::string>();

    if (ty->isVoidTy()) {
      return;
    }

    // Grab the dedicated allocation block we created in ProgramGenerator
    llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock &entryBlock = parentFn->getEntryBlock();
    llvm::IRBuilder<> TmpBuilder(&entryBlock, entryBlock.begin());

    llvm::AllocaInst *alloca = TmpBuilder.CreateAlloca(ty, nullptr, name);
    TmpBuilder.CreateStore(llvm::Constant::getNullValue(ty), alloca);
    ctx.SymbolEnv.back()[name] = alloca;
    return;
  }

  if (op == "assign") {
    llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *target = ctx.resolveSymbol(dst);

    if (!target) {
      if (val && !val->getType()->isVoidTy()) {
        ctx.Error.fatal("Assignment to unknown variable: " + dst, stmt);
      }
      return;
    }

    if (!val || val->getType()->isVoidTy()) {
      return;
    }

    ctx.Builder->CreateStore(val, target);
    return;
  }

  if (op == "struct_init") {
    std::string dst = stmt.value("dst", "");
    int field_index = stmt.value("field_index", 0);

    llvm::Value *dstPtr = ctx.resolveSymbol(dst);
    if (!dstPtr) {
      ctx.Error.fatal("struct_init: target stack allocation not found for '" + dst + "'", stmt);
      return;
    }

    // Derive type from the alloca (most reliable source)
    llvm::Type *structTy = nullptr;
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(dstPtr)) {
      structTy = alloca->getAllocatedType();
    } else {
      ctx.Error.fatal("struct_init: target is not an alloca", stmt);
      return;
    }

    llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
    if (!val) return;

    llvm::Value *fieldGep = ctx.Builder->CreateStructGEP(structTy, dstPtr, field_index, dst + "_init_gep");
    ctx.Builder->CreateStore(val, fieldGep);
    return;
  }

  // -------------------------------------------------------------------------
  // array_init  — initializes one element of an already-declared array alloca.
  //
  // JSON shape (from export.go):
  //   { "op": "array_init",
  //     "dst": "_t1",     // name of the array temporary
  //     "index": 0,       // zero-based element position
  //     "value": { ... }  // flat value expression
  //   }
  //
  // We resolve the array alloca from the symbol table, GEP to the element
  // using a two-index chain (outer array pointer + element offset), evaluate
  // the value expression, and store the result.
  // -------------------------------------------------------------------------
  if (op == "array_init") {
    std::string dst = stmt["dst"].get<std::string>();
    int index = stmt["index"].get<int>();

    llvm::Value *arrayPtr = ctx.resolveSymbol(dst);
    if (!arrayPtr) {
      ctx.Error.fatal("array_init: unknown array temporary '" + dst + "'", stmt);
      return;
    }
    auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(arrayPtr);
    if (!alloca) {
      ctx.Error.fatal("array_init: symbol '" + dst + "' is not an alloca", stmt);
      return;
    }
    llvm::Type *arrayTy = alloca->getAllocatedType();

    llvm::Value *elemVal = evaluateExpression(ctx, stmt["value"]);
    if (!elemVal) return;

    // Two-index GEP: first index strips the outer pointer, second selects the element.
    llvm::Value *elemPtr =
        ctx.Builder->CreateGEP(arrayTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(index)},
                               dst + "[" + std::to_string(index) + "]");
    ctx.Builder->CreateStore(elemVal, elemPtr);
    return;
  }

  // -------------------------------------------------------------------------
  // slice_read — extracts a slice header from an array, string, or vector.
  // -------------------------------------------------------------------------
  if (op == "slice_read") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *dstPtr = ctx.resolveSymbol(dst);
    if (!dstPtr) {
      ctx.Error.fatal("slice_read: unknown destination temporary '" + dst + "'", stmt);
      return;
    }

    llvm::Value *leftVal = evaluateExpression(ctx, stmt["left"]);
    if (!leftVal) return;

    llvm::Type *leftTy = llvmTypeFor(ctx, stmt["container_type"]);
    llvm::Value *leftPtr = leftVal;

    // If leftVal is a loaded value, spill it to memory so we can safely GEP
    if (!leftVal->getType()->isPointerTy()) {
      llvm::AllocaInst *spill = ctx.Builder->CreateAlloca(leftTy, nullptr, "slice_source_spill");
      ctx.Builder->CreateStore(leftVal, spill);
      leftPtr = spill;
    } else {
      // Re-resolve the raw alloca if it's an identifier to avoid loading the pointer
      if (stmt["left"].contains("value")) {
        std::string leftName = stmt["left"]["value"].get<std::string>();
        if (llvm::Value *sym = ctx.resolveSymbol(leftName)) {
          if (llvm::isa<llvm::AllocaInst>(sym)) {
            leftPtr = sym;
          }
        }
      }
    }

    llvm::Value *lowVal = stmt.contains("low") && !stmt["low"].is_null() ? evaluateExpression(ctx, stmt["low"])
                                                                         : ctx.Builder->getInt32(0);
    llvm::Value *highVal = nullptr;

    llvm::Value *originalCap = nullptr;
    llvm::Value *originalRawPtr = nullptr;
    llvm::Value *originalDataPtr = nullptr;

    // Note: Use lowercase kinds because lowerType() exports them as "array", "slice", etc.
    std::string_view leftKind = stmt["container_type"]["kind"].get<std::string_view>();

    if (leftKind == "array") {
      int size = stmt["container_type"]["size"].get<int>();
      originalCap = ctx.Builder->getInt32(size);

      // Get the pointer to the first element of the array
      originalDataPtr = ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)},
                                               "array_data_ptr");

      // FIX: Fixed-size arrays are stack allocated and do NOT have an ARC header.
      // Force the raw_ptr to null so maml_retain safely ignores it.
      originalRawPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));

      highVal = stmt.contains("high") && !stmt["high"].is_null() ? evaluateExpression(ctx, stmt["high"]) : originalCap;

    } else if (leftKind == "string") {
      llvm::Value *dataGep =
          ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}, "str_data_gep");
      llvm::Value *lenGep =
          ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}, "str_len_gep");

      originalRawPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
      originalDataPtr = originalRawPtr;
      originalCap = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);

      highVal = stmt.contains("high") && !stmt["high"].is_null() ? evaluateExpression(ctx, stmt["high"]) : originalCap;

    } else if (leftKind == "slice" || leftKind == "vector") {
      llvm::Value *rawGep =
          ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)});
      llvm::Value *dataGep =
          ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)});
      llvm::Value *lenGep =
          ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)});
      llvm::Value *capGep =
          ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)});

      originalRawPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), rawGep);
      originalDataPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
      originalCap = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), capGep);

      highVal = stmt.contains("high") && !stmt["high"].is_null()
                    ? evaluateExpression(ctx, stmt["high"])
                    : ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
    } else {
      ctx.Error.fatal("slice_read: unrecognised container kind '" + std::string(leftKind) + "'", stmt);
      return;
    }

    llvm::Value *newLen = ctx.Builder->CreateSub(highVal, lowVal, "slice_len");
    llvm::Value *newCap = ctx.Builder->CreateSub(originalCap, lowVal, "slice_cap");

    llvm::Type *baseTy = llvmTypeFor(ctx, stmt["container_type"]["elem_type"]);
    llvm::Value *newDataPtr = ctx.Builder->CreateGEP(baseTy, originalDataPtr, lowVal, "slice_data_ptr");

    llvm::Type *sliceTy = llvmTypeFor(ctx, stmt["result_type"]);

    // Write the 4 fat-pointer fields into the pre-allocated slice struct
    ctx.Builder->CreateStore(
        originalRawPtr, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
    ctx.Builder->CreateStore(
        newDataPtr, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
    ctx.Builder->CreateStore(
        newLen, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)}));
    ctx.Builder->CreateStore(
        newCap, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)}));

    // ARC Retention: Bump the ref count on the underlying heap buffer if necessary
    if (leftKind == "slice" || leftKind == "vector" || leftKind == "string" || leftKind == "array") {
      llvm::FunctionCallee retainFn = ctx.Module->getOrInsertFunction(
          "maml_retain", llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                                 {llvm::PointerType::getUnqual(ctx.Context)}, false));
      ctx.Builder->CreateCall(retainFn, {originalRawPtr});
    }

    return;
  }

  if (op == "field_read") {
    std::string dst = stmt.value("dst", "");
    int field_index = stmt.value("field_index", 0);
    auto objectJson = stmt["object"];

    std::string objName;
    if (objectJson.contains("value") && objectJson["value"].is_string()) {
      objName = objectJson["value"].get<std::string>();
    } else {
      ctx.Error.fatal("field_read: object must be an identifier", stmt);
      return;
    }

    llvm::Value *objVal = ctx.resolveSymbol(objName);
    if (!objVal) {
      ctx.Error.fatal("field_read: object '" + objName + "' not found", stmt);
      return;
    }

    llvm::Value *objPtr = nullptr;
    llvm::Type *structTy = nullptr;

    // Case 1: Object is still an alloca (most common base case)
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(objVal)) {
      objPtr = objVal;
      structTy = alloca->getAllocatedType();
    }
    // Case 2: Object is a previously loaded struct value (nested field access)
    else if (objVal->getType()->isStructTy()) {
      // We need to spill the loaded struct back to a temporary alloca so we can GEP it
      llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
      llvm::IRBuilder<> TmpBuilder(&F->getEntryBlock(), F->getEntryBlock().begin());

      llvm::AllocaInst *spillAlloca = TmpBuilder.CreateAlloca(objVal->getType(), nullptr, objName + "_spill");

      ctx.Builder->CreateStore(objVal, spillAlloca);

      objPtr = spillAlloca;
      structTy = objVal->getType();
    } else {
      ctx.Error.fatal("field_read: object '" + objName + "' is neither alloca nor struct value", stmt);
      return;
    }

    // Now perform the GEP + load
    llvm::Value *fieldGep = ctx.Builder->CreateStructGEP(structTy, objPtr, field_index, dst + "_gep");

    llvm::Type *fieldTy = llvmTypeFor(ctx, stmt["type"]);
    llvm::Value *loadedVal = ctx.Builder->CreateLoad(fieldTy, fieldGep, dst + "_val");

    ctx.SymbolEnv.back()[dst] = loadedVal;
    return;
  }

  if (op == "index_assign") {
    std::string targetName = stmt["target"].get<std::string>();
    llvm::Value *arrayPtr = ctx.resolveSymbol(targetName);
    if (!arrayPtr) {
      ctx.Error.fatal("index_assign: unknown target '" + targetName + "'", stmt);
      return;
    }
    auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(arrayPtr);
    if (!alloca) {
      ctx.Error.fatal("index_assign: target '" + targetName + "' is not an alloca", stmt);
      return;
    }

    const auto &targetTypeJson = stmt["target_type"];
    std::string_view kind = "unknown";
    if (targetTypeJson.is_object() && targetTypeJson.contains("kind")) {
      kind = targetTypeJson["kind"].get<std::string_view>();
    }

    llvm::Value *indexVal = evaluateExpression(ctx, stmt["index"]);
    llvm::Value *storeVal = evaluateExpression(ctx, stmt["value"]);

    llvm::Value *elemPtr = nullptr;

    if (kind == "array") {
      llvm::Type *arrayTy = llvmTypeFor(ctx, targetTypeJson);
      elemPtr = ctx.Builder->CreateGEP(arrayTy, arrayPtr, {ctx.Builder->getInt32(0), indexVal}, "index_assign_ptr");

    } else if (kind == "slice" || kind == "vector") {
      llvm::Type *hdrTy = llvmTypeFor(ctx, targetTypeJson);
      llvm::Value *dataPtrGep = ctx.Builder->CreateGEP(
          hdrTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}, "slice_data_gep");
      llvm::Value *dataPtr =
          ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "slice_data_ptr");
      // elem_type is on the DTO value expression's type — use the stored value's type
      llvm::Type *elemTy = storeVal->getType();
      elemPtr = ctx.Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_assign_ptr");

    } else {
      ctx.Error.fatal("index_assign: unrecognised container kind '" + std::string(kind) + "'", stmt);
      return;
    }

    ctx.Builder->CreateStore(storeVal, elemPtr);
    return;
  }

  if (op == "copy" || op == "move") {
    std::string dst = stmt["dst"].get<std::string>();
    std::string src = stmt["src"].get<std::string>();

    llvm::Value *dstVal = ctx.resolveSymbol(dst);
    llvm::Value *srcVal = ctx.resolveSymbol(src);

    if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(srcVal)) {
      llvm::Value *loaded = ctx.Builder->CreateLoad(srcAlloca->getAllocatedType(), srcAlloca);
      ctx.Builder->CreateStore(loaded, dstVal);
    }
    return;
  }

  // -------------------------------------------------------------------------
  // cast — explicitly converts a value to a different type representation.
  // -------------------------------------------------------------------------
  if (op == "cast") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *srcVal = evaluateExpression(ctx, stmt["src"]);
    llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);
    llvm::Value *castVal = nullptr;

    if (srcVal->getType()->isIntegerTy() && targetTy->isIntegerTy()) {
      // e.g., zero-extending our i32 map key to an i64 for the hash
      castVal = ctx.Builder->CreateZExtOrTrunc(srcVal, targetTy, dst + "_cast");
    } else if (srcVal->getType()->isPointerTy() && targetTy->isPointerTy()) {
      castVal = ctx.Builder->CreatePointerCast(srcVal, targetTy, dst + "_cast");
    } else if (srcVal->getType()->isIntegerTy() && targetTy->isPointerTy()) {
      castVal = ctx.Builder->CreateIntToPtr(srcVal, targetTy, dst + "_cast");
    } else if (srcVal->getType()->isPointerTy() && targetTy->isIntegerTy()) {
      castVal = ctx.Builder->CreatePtrToInt(srcVal, targetTy, dst + "_cast");
    } else {
      ctx.Error.fatal("cast: unsupported cast operation", stmt);
      return;
    }

    ctx.SymbolEnv.back()[dst] = castVal;
    return;
  }

  // -------------------------------------------------------------------------
  // load_ptr — dereferences an opaque pointer into a typed value.
  // -------------------------------------------------------------------------
  if (op == "load_ptr") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *ptrVal = evaluateExpression(ctx, stmt["ptr"]);
    llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);

    // Evaluate the raw pointer, and execute a typed load
    llvm::Value *loadedVal = ctx.Builder->CreateLoad(targetTy, ptrVal, dst + "_load");

    // Bind to the destination register
    ctx.SymbolEnv.back()[dst] = loadedVal;
    return;
  }

  // -------------------------------------------------------------------------
  // store — writes a value directly into a destination pointer address.
  // -------------------------------------------------------------------------
  if (op == "store") {
    llvm::Value *val = evaluateExpression(ctx, stmt["value"]);
    std::string dst_ptr_name = stmt["dst_ptr"].get<std::string>();

    llvm::Value *dstPtr = ctx.resolveSymbol(dst_ptr_name);
    if (!dstPtr) {
      ctx.Error.fatal("store: destination pointer not found: " + dst_ptr_name, stmt);
      return;
    }

    ctx.Builder->CreateStore(val, dstPtr);
    return;
  }

  if (op == "ref_alloc") {
    llvm::Type *ty = llvmTypeFor(ctx, stmt["type"]);
    std::string dst = stmt["dst"].get<std::string>();

    llvm::Function *allocFn = ctx.Module->getFunction(rt::ALLOC);
    llvm::DataLayout DL(ctx.Module.get());
    uint64_t size = DL.getTypeAllocSize(ty);
    llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), size);

    llvm::Value *heapPtr = ctx.Builder->CreateCall(allocFn, {sizeVal}, dst + "_heap");
    llvm::Value *dstTarget = ctx.resolveSymbol(dst);
    ctx.Builder->CreateStore(heapPtr, dstTarget);
    return;
  }

  if (op == "ref_inc") {
    std::string src = stmt["src"].get<std::string>();
    if (llvm::Value *srcTarget = ctx.resolveSymbol(src)) {
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(srcTarget)) {
        llvm::Value *loaded = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca);
        llvm::Function *retainFn = ctx.Module->getFunction(rt::RETAIN);
        if (retainFn) ctx.Builder->CreateCall(retainFn, {loaded});
      }
    }
    return;
  }

  if (op == "ref_dec") {
    std::string src = stmt["src"].get<std::string>();
    if (llvm::Value *srcTarget = ctx.resolveSymbol(src)) {
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(srcTarget)) {
        llvm::Value *loaded = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca);
        llvm::Function *releaseFn = ctx.Module->getFunction(rt::RELEASE);
        if (releaseFn) ctx.Builder->CreateCall(releaseFn, {loaded});
      }
    }
    return;
  }

  if (op == "variant_discriminant") {
    std::string dst = stmt["dst"].get<std::string>();
    auto objectJson = stmt["object"];

    // 1. Evaluate the underlying variant object to get its pointer
    llvm::Value *objectPtr = evaluateExpression(ctx, objectJson);
    if (!objectPtr) {
      ctx.Error.fatal("Failed to evaluate object for variant_discriminant", stmt);
      return;
    }

    // 2. Resolve the underlying LLVM structural type of the SumType
    // using the calibration pattern established in Phase 1
    llvm::Type *sumTy = llvmTypeFor(ctx, objectJson["type"]);

    // 3. The discriminant is structurally guaranteed to be at index 0
    // Use an explicit type-safe GEP calculation compatible with opaque pointers
    llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(sumTy, objectPtr, 0, dst + "_gep");

    // 4. Load the i32 tag value out of the pointer target
    llvm::Value *discrimVal = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), discrimGep, dst + "_val");

    // 5. Track the loaded discriminant value in our local value registry
    ctx.SymbolEnv.back()[dst] = discrimVal;

    return;
  }

  if (op == "variant_read") {
    std::string dst = stmt.value("dst", "");
    std::string variant_name = stmt.value("variant_name", "");
    int payload_index = stmt.value("payload_index", 0);
    auto objectJson = stmt["object"];

    // 1. Evaluate the underlying variant object pointer
    llvm::Value *objectPtr = evaluateExpression(ctx, objectJson);
    if (!objectPtr) {
      ctx.Error.fatal("Failed to evaluate object for variant_read", stmt);
      return;
    }

    // 2. Reconstruct the precise layout for this specific variant.
    // This ensures LLVM can accurately compute structural field offsets.
    std::vector<llvm::Type *> fieldTys;
    fieldTys.push_back(llvm::Type::getInt32Ty(ctx.Context));  // Element 0 is always the discriminant

    // Look up the variant payload signature from the object type's metadata
    if (objectJson.contains("type") && objectJson["type"].contains("variants")) {
      for (const auto &v : objectJson["type"]["variants"]) {
        if (v.value("name", "") == variant_name) {
          if (v.contains("tuple_types")) {
            for (const auto &tJson : v["tuple_types"]) {
              fieldTys.push_back(llvmTypeFor(ctx, tJson));
            }
          }
          if (v.contains("fields")) {
            for (const auto &fJson : v["fields"]) {
              fieldTys.push_back(llvmTypeFor(ctx, fJson["type"]));
            }
          }
          break;
        }
      }
    }

    // Failsafe: If type metadata is sparse, backfill up to the index with the target type
    if (fieldTys.size() <= static_cast<size_t>(1 + payload_index)) {
      while (fieldTys.size() <= static_cast<size_t>(1 + payload_index)) {
        fieldTys.push_back(llvmTypeFor(ctx, stmt["type"]));
      }
    }

    // 3. Create an anonymous structural representation for this variant's active layout
    llvm::StructType *variantStructTy = llvm::StructType::get(ctx.Context, fieldTys, /*isPacked=*/false);

    // 4. Calculate the type-safe GEP into the payload buffer element
    // Offset is 1 + payload_index to step past the discriminant at index 0
    llvm::Value *payloadGep = ctx.Builder->CreateStructGEP(variantStructTy, objectPtr, 1 + payload_index, dst + "_gep");

    // 5. Load the payload value using its true LLVM type
    llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);
    llvm::Value *loadedVal = ctx.Builder->CreateLoad(targetTy, payloadGep, dst + "_val");

    // 6. Map the value to the destination register name
    ctx.SymbolEnv.back()[dst] = loadedVal;
    return;
  }

  if (op == "variant_init") {
    std::string dst = stmt.value("dst", "");
    std::string variant_name = stmt.value("variant_name", "");
    int discriminant = stmt.value("discriminant", 0);
    auto payloadsJson = stmt["payloads"];

    // 1. Locate the pre-allocated target memory block pointer
    llvm::Value *dstPtr = ctx.resolveSymbol(dst);
    if (!dstPtr) {
      ctx.Error.fatal("variant_init: target stack allocation pointer not found for '" + dst + "'", stmt);
      return;
    }

    // 2. Dynamically reconstruct the precise structural schema layout for this variant.
    // Element 0 is always the i32 discriminant.
    std::vector<llvm::Type *> fieldTys;
    fieldTys.push_back(llvm::Type::getInt32Ty(ctx.Context));

    // Gather structural types for every tracked payload parameter
    for (const auto &pJson : payloadsJson) {
      fieldTys.push_back(llvmTypeFor(ctx, pJson["type"]));
    }

    // Bind types together into an anonymous LLVM layout schema
    llvm::StructType *variantStructTy = llvm::StructType::get(ctx.Context, fieldTys, /*isPacked=*/false);

    // 3. Compute the structural GEP for index 0 and write the tag discriminant value
    llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(variantStructTy, dstPtr, 0, dst + "_disc_gep");
    llvm::Value *discrimVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), discriminant);
    ctx.Builder->CreateStore(discrimVal, discrimGep);

    // 4. Sequentially evaluate and store each active payload item
    for (size_t i = 0; i < payloadsJson.size(); ++i) {
      auto pJson = payloadsJson[i];

      // Evaluate the sub-expression to yield a live LLVM value
      llvm::Value *payloadVal = evaluateExpression(ctx, pJson);
      if (!payloadVal) {
        ctx.Error.fatal("variant_init: Failed to evaluate payload field index " + std::to_string(i), stmt);
        continue;
      }

      // Offset the index by +1 to skip past the element 0 discriminant tag slot
      llvm::Value *payloadGep =
          ctx.Builder->CreateStructGEP(variantStructTy, dstPtr, 1 + i, dst + "_pld_" + std::to_string(i) + "_gep");

      // Safely commit the value directly into the variant allocation
      ctx.Builder->CreateStore(payloadVal, payloadGep);
    }
    return;
  }

  if (op == "coro_prologue") {
    // (Coroutine intrinsics setup goes here)
    return;
  }

  if (op == "binary_op") {
    std::string dst = stmt["dst"].get<std::string>();
    std::string_view opSymbol = stmt["operator"].get<std::string_view>();
    llvm::Value *left = evaluateExpression(ctx, stmt["left"]);
    llvm::Value *right = evaluateExpression(ctx, stmt["right"]);
    llvm::Value *result = nullptr;

    if (opSymbol == "/" || opSymbol == "%") {
      llvm::Value *isZero = ctx.Builder->CreateICmpEQ(right, llvm::ConstantInt::get(right->getType(), 0), "is_zero");
      llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
      llvm::BasicBlock *trapBB = llvm::BasicBlock::Create(ctx.Context, "trap_div_zero", F);
      llvm::BasicBlock *contBB = llvm::BasicBlock::Create(ctx.Context, "cont_div", F);

      ctx.Builder->CreateCondBr(isZero, trapBB, contBB);
      ctx.Builder->SetInsertPoint(trapBB);
      llvm::Function *trapFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::trap);
      ctx.Builder->CreateCall(trapFn);
      ctx.Builder->CreateUnreachable();

      ctx.Builder->SetInsertPoint(contBB);
      if (opSymbol == "/") result = ctx.Builder->CreateSDiv(left, right, "divtmp");
      if (opSymbol == "%") result = ctx.Builder->CreateSRem(left, right, "modtmp");
    } else if (opSymbol == "+") {
      result = ctx.Builder->CreateAdd(left, right, "addtmp");
    } else if (opSymbol == "-") {
      result = ctx.Builder->CreateSub(left, right, "subtmp");
    } else if (opSymbol == "*") {
      result = ctx.Builder->CreateMul(left, right, "multmp");
    } else if (opSymbol == "==") {
      result = ctx.Builder->CreateICmpEQ(left, right, "eqtmp");
    } else if (opSymbol == "!=") {
      result = ctx.Builder->CreateICmpNE(left, right, "neqtmp");
    } else if (opSymbol == "<") {
      result = ctx.Builder->CreateICmpSLT(left, right, "lttmp");
    } else if (opSymbol == ">") {
      result = ctx.Builder->CreateICmpSGT(left, right, "gttmp");
    } else if (opSymbol == "<=") {
      result = ctx.Builder->CreateICmpSLE(left, right, "letmp");
    } else if (opSymbol == ">=") {
      result = ctx.Builder->CreateICmpSGE(left, right, "getmp");
    } else {
      ctx.Error.fatal("Unknown binary operator: " + std::string(opSymbol), stmt);
    }

    ctx.SymbolEnv.back()[dst] = result;
    return;
  }

  if (op == "unary_op") {
    std::string dst = stmt["dst"].get<std::string>();
    std::string_view opSymbol = stmt["operator"].get<std::string_view>();
    llvm::Value *operand = evaluateExpression(ctx, stmt["operand"]);
    llvm::Value *result = nullptr;

    if (opSymbol == "!") {
      result = ctx.Builder->CreateXor(operand, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), "nottmp");
    } else if (opSymbol == "-") {
      result =
          ctx.Builder->CreateSub(llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0), operand, "negtmp");
    } else {
      ctx.Error.fatal("Unknown unary operator: " + std::string(opSymbol), stmt);
    }

    ctx.SymbolEnv.back()[dst] = result;
    return;
  }

  if (op == "index_read") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *leftVal = evaluateExpression(ctx, stmt["source"]);
    llvm::Value *indexVal = evaluateExpression(ctx, stmt["index"]);
    llvm::Type *elemTy = llvmTypeFor(ctx, stmt["type"]);

    llvm::Value *leftPtr = leftVal;
    if (stmt["source"].contains("value")) {
      std::string leftName = stmt["source"]["value"].get<std::string>();
      if (llvm::Value *sym = ctx.resolveSymbol(leftName)) {
        if (llvm::isa<llvm::AllocaInst>(sym)) {
          leftPtr = sym;
        }
      }
    }

    const auto &leftTypeJson = stmt["source_type"];
    std::string_view kind = "unknown";
    if (leftTypeJson.is_string()) {
      kind = leftTypeJson.get<std::string_view>();
    } else if (leftTypeJson.is_object() && leftTypeJson.contains("kind")) {
      kind = leftTypeJson["kind"].get<std::string_view>();
    }

    llvm::Value *elemPtr = nullptr;
    if (kind == "array") {
      llvm::Type *arrayTy = llvmTypeFor(ctx, leftTypeJson);
      elemPtr = ctx.Builder->CreateGEP(arrayTy, leftPtr, {ctx.Builder->getInt32(0), indexVal}, "array_elem_ptr");
    } else if (kind == "string") {
      llvm::Type *strTy = llvmTypeFor(ctx, leftTypeJson);
      llvm::Value *dataPtrGep =
          ctx.Builder->CreateGEP(strTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}, "str_data_gep");
      llvm::Value *dataPtr =
          ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "str_data_ptr");
      elemPtr = ctx.Builder->CreateGEP(llvm::Type::getInt8Ty(ctx.Context), dataPtr, indexVal, "char_ptr");
    } else if (kind == "slice") {
      llvm::Type *hdrTy = llvmTypeFor(ctx, leftTypeJson);
      llvm::Value *dataPtrGep = ctx.Builder->CreateGEP(
          hdrTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}, "slice_data_gep");
      llvm::Value *dataPtr =
          ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "slice_data_ptr");
      elemPtr = ctx.Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_elem_ptr");
    } else {
      ctx.Error.fatal("index_read: unrecognised container kind '" + std::string(kind) + "'", stmt);
      return;
    }

    llvm::Value *loadedVal = ctx.Builder->CreateLoad(elemTy, elemPtr, "elem_load");
    ctx.SymbolEnv.back()[dst] = loadedVal;
    return;
  }

  if (op == "call_inst") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *callee = nullptr;
    std::string funcName;
    std::string_view functionNodeType = "unknown";

    if (stmt["function"].contains("op")) {
      functionNodeType = stmt["function"]["op"].get<std::string_view>();
    }

    if (functionNodeType == "ident") {
      funcName = stmt["function"]["value"].get<std::string>();
      callee = ctx.Module->getFunction(funcName);
      if (!callee) {
        callee = ctx.resolveSymbol(funcName);
      }
    } else {
      callee = evaluateExpression(ctx, stmt["function"]);
    }

    if (!callee) {
      ctx.Error.fatal("Could not resolve function for call", stmt);
      return;
    }

    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(callee)) {
      callee = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca, "fn_ptr_load");
    }

    llvm::FunctionType *FT = nullptr;
    if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
      FT = F->getFunctionType();
    }

    std::vector<llvm::Value *> args;
    size_t i = 0;
    for (const auto &argWrapper : stmt["arguments"]) {
      llvm::Value *argVal = evaluateExpression(ctx, argWrapper["argument"]);
      if (!argVal) return;

      if (FT && i < FT->getNumParams()) {
        llvm::Type *expectedTy = FT->getParamType(i);
        llvm::Type *actualTy = argVal->getType();

        if (expectedTy != actualTy) {
          if (expectedTy->isPointerTy() && actualTy->isStructTy()) {
            argVal = ctx.Builder->CreateExtractValue(argVal, {0}, "fat_ptr_unwrap");
          } else if (expectedTy->isIntegerTy() && actualTy->isIntegerTy()) {
            argVal = ctx.Builder->CreateIntCast(argVal, expectedTy, true, "arg_cast");
          } else if (expectedTy->isPointerTy() && actualTy->isPointerTy()) {
            argVal = ctx.Builder->CreatePointerCast(argVal, expectedTy, "ptr_cast");

            // 🌟 DIRECT FIX: Implement the missing Auto-Spilling and Null coercion
          } else if (expectedTy->isPointerTy() && !actualTy->isPointerTy()) {
            // Case A: The frontend sent `0` as a fake null pointer (e.g., for empty string keys)
            if (auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(argVal); cInt && cInt->isZero()) {
              argVal = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(expectedTy));
            }
            // Case B: The frontend sent a raw primitive (like `100`). Auto-spill it to the stack.
            else {
              // Allocate in the entry block to prevent stack overflows in loops
              llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
              llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());

              llvm::AllocaInst *spill = TmpBuilder.CreateAlloca(actualTy, nullptr, "arg_spill");
              ctx.Builder->CreateStore(argVal, spill);
              argVal = spill;  // Now we pass the pointer to the spilled value!
            }
          }
        }
      }
      args.push_back(argVal);
      i++;
    }

    llvm::CallInst *callResult;
    if (FT && FT->getReturnType()->isVoidTy()) {
      // Pass no name for void functions
      callResult = ctx.Builder->CreateCall(FT, callee, args);
    } else {
      // Safely name functions that return a value
      callResult = ctx.Builder->CreateCall(FT, callee, args, "calltmp");
    }

    // Only bind to the Symbol Environment if the function actually returns a value
    if (!callResult->getType()->isVoidTy()) {
      // 🛡️ GUARD 2: Return Type Boundary Verification
      llvm::Type *expectedRetTy = llvmTypeFor(ctx, stmt["type"]);
      if (callResult->getType() != expectedRetTy) {
        ctx.Error.fatal("Return type violation in call to '" + funcName +
                            "'. MIR expected a different LLVM type than the ABI returned.",
                        stmt);
        return;
      }

      ctx.SymbolEnv.back()[dst] = callResult;
    }
    return;
  }

  ctx.Error.fatal("Unknown instruction op: " + std::string(op), stmt);
}

void compileTerminator(CodegenContext &ctx, const nlohmann::json &term) {
  if (term.is_null()) return;

  std::string_view op = term["op"].get<std::string_view>();

  if (op == "ret") {
    // Look for the dedicated _ret temporary allocated by the frontend
    if (llvm::Value *retTarget = ctx.resolveSymbol("_ret")) {
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(retTarget)) {
        llvm::Value *retVal = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca);
        ctx.Builder->CreateRet(retVal);
        return;
      }
    }
    ctx.Builder->CreateRetVoid();
    return;
  }

  if (op == "br") {
    int target = std::stoi(term["target"].get<std::string>());
    ctx.Builder->CreateBr(ctx.Blocks[target]);
    return;
  }

  if (op == "cond_br") {
    llvm::Value *condVal = evaluateExpression(ctx, term["condition"]);
    int trueTarget = std::stoi(term["true_target"].get<std::string>());
    int falseTarget = std::stoi(term["false_target"].get<std::string>());
    ctx.Builder->CreateCondBr(condVal, ctx.Blocks[trueTarget], ctx.Blocks[falseTarget]);
    return;
  }

  if (op == "unreachable") {
    ctx.Builder->CreateUnreachable();
    return;
  }

  ctx.Error.fatal("Unknown terminator op: " + std::string(op), term);
}

}  // namespace maml