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

  // -------------------------------------------------------------------------
  // struct_init  — initializes one field of an already-declared struct alloca.
  //
  // JSON shape (from export.go):
  //   { "op": "struct_init",
  //     "dst": "_t1",          // name of the struct temporary
  //     "field_name": "x",     // source-level field name (for diagnostics)
  //     "field_index": 0,      // declaration-order GEP index
  //     "value": { ... }       // flat value expression
  //   }
  //
  // We resolve the struct alloca from the symbol table, GEP to the field
  // using the pre-resolved "field_index", evaluate the value expression, and
  // store the result.  This is the write side of the struct layout pair;
  // field_read (below) is the read side.
  // -------------------------------------------------------------------------
  if (op == "struct_init") {
    std::string dst = stmt["dst"].get<std::string>();
    int fieldIndex = stmt["field_index"].get<int>();

    llvm::Value *structPtr = ctx.resolveSymbol(dst);
    if (!structPtr) {
      ctx.Error.fatal("struct_init: unknown struct temporary '" + dst + "'", stmt);
      return;
    }

    auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(structPtr);
    if (!alloca) {
      ctx.Error.fatal("struct_init: symbol '" + dst + "' is not an alloca", stmt);
      return;
    }

    // Default to the allocated type (e.g., the { i32, [N x i8] } base type)
    llvm::Type *gepTy = alloca->getAllocatedType();

    // --- PHASE 4: Override GEP Type with Variant Layout ---
    if (stmt.contains("variant_layout") && !stmt["variant_layout"].is_null()) {
      std::vector<llvm::Type *> elementTypes;
      for (const auto &tyJson : stmt["variant_layout"]) {
        elementTypes.push_back(llvmTypeFor(ctx, tyJson));
      }
      gepTy = llvm::StructType::get(ctx.Context, elementTypes, /*isPacked=*/false);
    }

    llvm::Value *fieldVal = evaluateExpression(ctx, stmt["value"]);
    if (!fieldVal) return;

    // Because pointers are opaque, we just pass gepTy directly!
    llvm::Value *fieldPtr =
        ctx.Builder->CreateGEP(gepTy, structPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(fieldIndex)},
                               dst + "." + stmt["field_name"].get<std::string>());

    ctx.Builder->CreateStore(fieldVal, fieldPtr);
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

  // -------------------------------------------------------------------------
  // field_read  — loads one field from a struct alloca into a new temporary.
  //
  // JSON shape (from export.go):
  //   { "op": "field_read",
  //     "dst": "_t2",          // name of the destination temporary
  //     "object": { "op": "ident", "value": "_t1" },
  //     "field_name": "x",     // source-level field name (for diagnostics)
  //     "field_index": 0,      // declaration-order GEP index
  //     "type": "i32"          // LLVM type of the loaded field
  //   }
  //
  // The destination temporary was already declared (and its alloca emitted)
  // by the preceding temp_decl instruction.  We GEP into the object's struct
  // layout using the pre-resolved index and store the loaded value into the
  // destination alloca.
  // -------------------------------------------------------------------------
  if (op == "field_read") {
    std::string dst = stmt["dst"].get<std::string>();
    int fieldIndex = stmt["field_index"].get<int>();
    llvm::Type *fieldType = llvmTypeFor(ctx, stmt["type"]);

    llvm::Value *dstPtr = ctx.resolveSymbol(dst);
    if (!dstPtr) {
      ctx.Error.fatal("field_read: unknown destination temporary '" + dst + "'", stmt);
      return;
    }

    llvm::Value *objVal = evaluateExpression(ctx, stmt["object"]);
    if (!objVal) return;

    // evaluateExpression on an ident loads through the alloca; we need the
    // raw pointer for GEP.  If the result is NOT a pointer already, spill it.
    llvm::Value *objPtr = objVal;
    if (!objVal->getType()->isPointerTy()) {
      // Determine the struct type so we can create a properly-typed spill.
      // We look up the object's name from the ident node, then inspect its
      // alloca type.
      if (stmt["object"].contains("value")) {
        std::string objName = stmt["object"]["value"].get<std::string>();
        if (llvm::Value *sym = ctx.resolveSymbol(objName)) {
          if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(sym)) {
            // Use the alloca directly — no load needed for a GEP source.
            objPtr = srcAlloca;
          }
        }
      }
      if (objPtr == objVal) {
        // Last resort: spill the loaded value into a fresh alloca.
        llvm::AllocaInst *spill = ctx.Builder->CreateAlloca(objVal->getType(), nullptr, "field_read_spill");
        ctx.Builder->CreateStore(objVal, spill);
        objPtr = spill;
      }
    } else {
      // objVal is already a pointer (alloca or loaded ptr).  For a struct
      // alloca, compileIdentifier loads through it — we need the raw alloca.
      // Re-resolve from the symbol table to get the unloaded alloca.
      if (stmt["object"].contains("value")) {
        std::string objName = stmt["object"]["value"].get<std::string>();
        if (llvm::Value *sym = ctx.resolveSymbol(objName)) {
          if (llvm::isa<llvm::AllocaInst>(sym)) {
            objPtr = sym;  // Use the alloca directly, skip the load.
          }
        }
      }
    }

    llvm::Type *gepTy = nullptr;
    if (auto *srcAlloca = llvm::dyn_cast<llvm::AllocaInst>(objPtr)) {
      gepTy = srcAlloca->getAllocatedType();
    }

    // --- PHASE 4: Override GEP Type with Variant Layout ---
    if (stmt.contains("variant_layout") && !stmt["variant_layout"].is_null()) {
      std::vector<llvm::Type *> elementTypes;
      for (const auto &tyJson : stmt["variant_layout"]) {
        elementTypes.push_back(llvmTypeFor(ctx, tyJson));
      }
      gepTy = llvm::StructType::get(ctx.Context, elementTypes, /*isPacked=*/false);
    } else if (!gepTy) {
      ctx.Error.fatal("field_read: cannot determine struct type for object", stmt);
      return;
    }

    // GEP using the concrete layout, bypassing the opaque array bounds
    llvm::Value *fieldPtr =
        ctx.Builder->CreateGEP(gepTy, objPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(fieldIndex)},
                               stmt["field_name"].get<std::string>() + "_ptr");

    llvm::Value *fieldVal =
        ctx.Builder->CreateLoad(fieldType, fieldPtr, stmt["field_name"].get<std::string>() + "_load");

    ctx.Builder->CreateStore(fieldVal, dstPtr);
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

  if (op == "coro_prologue") {
    // (Coroutine intrinsics setup goes here)
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