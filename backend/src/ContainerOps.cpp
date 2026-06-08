#include <llvm/IR/DataLayout.h>

#include <string>
#include <string_view>

#include "ExprGenerator.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

// -----------------------------------------------------------------------------
// Private, Isolated Instruction Helpers
// -----------------------------------------------------------------------------

static void handleArrayInit(CodegenContext &ctx, const nlohmann::json &stmt) {
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

  // Two-index GEP: first index strips the outer pointer, second selects the element offset
  llvm::Value *elemPtr =
      ctx.Builder->CreateGEP(arrayTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(index)},
                             dst + "[" + std::to_string(index) + "]");

  ctx.Builder->CreateStore(elemVal, elemPtr);
}
static void handleSliceRead(CodegenContext &ctx, const nlohmann::json &stmt) {
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

  if (stmt["left"].contains("value")) {
    std::string leftName = stmt["left"]["value"].get<std::string>();
    if (llvm::Value *sym = ctx.resolveSymbol(leftName)) {
      if (llvm::isa<llvm::AllocaInst>(sym)) {
        leftPtr = sym;
      }
    }
  }

  if (!leftPtr->getType()->isPointerTy()) {
    llvm::AllocaInst *spill = ctx.Builder->CreateAlloca(leftTy, nullptr, "slice_source_spill");
    ctx.Builder->CreateStore(leftVal, spill);
    leftPtr = spill;
  }

  llvm::Value *lowVal =
      stmt.contains("low") && !stmt["low"].is_null() ? evaluateExpression(ctx, stmt["low"]) : ctx.Builder->getInt32(0);

  llvm::Value *highVal = nullptr;
  llvm::Value *originalCap = nullptr;
  llvm::Value *originalRawPtr = nullptr;
  llvm::Value *originalDataPtr = nullptr;

  std::string_view leftKind = stmt["container_type"]["kind"].get<std::string_view>();

  if (leftKind == "array") {
    int size = stmt["container_type"]["size"].get<int>();
    originalCap = ctx.Builder->getInt32(size);
    originalDataPtr =
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}, "array_data_ptr");
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

  } else if (leftKind == "vector") {
    // Vector variable holds an opaque pointer handle to the heap. Load it first.
    llvm::Value *vecHandle = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), leftPtr, "vec_handle");

    // Map the true structural layout of the runtime's on-heap VecHeader for GEP resolution
    llvm::StructType *hdrTy =
        llvm::StructType::get(ctx.Context, {
                                               llvm::PointerType::getUnqual(ctx.Context),  // offset 0: raw_ptr
                                               llvm::PointerType::getUnqual(ctx.Context),  // offset 8: data_ptr
                                               llvm::Type::getInt32Ty(ctx.Context),        // offset 16: len
                                               llvm::Type::getInt32Ty(ctx.Context),        // offset 20: cap
                                               llvm::Type::getInt32Ty(ctx.Context),        // offset 24: item_size
                                               llvm::Type::getInt32Ty(ctx.Context)         // offset 28: _pad
                                           });

    llvm::Value *rawGep =
        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)});
    llvm::Value *dataGep =
        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)});
    llvm::Value *lenGep =
        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)});
    llvm::Value *capGep =
        ctx.Builder->CreateGEP(hdrTy, vecHandle, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)});

    originalRawPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), rawGep);
    originalDataPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
    originalCap = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), capGep);
    highVal = stmt.contains("high") && !stmt["high"].is_null()
                  ? evaluateExpression(ctx, stmt["high"])
                  : ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);

  } else if (leftKind == "view") {
    // View is a stack-allocated struct variable. GEP into its local fields directly.
    llvm::Value *rawGep = ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)});
    llvm::Value *dataGep =
        ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)});
    llvm::Value *lenGep = ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)});
    llvm::Value *capGep = ctx.Builder->CreateGEP(leftTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)});

    originalRawPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), rawGep);
    originalDataPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataGep);
    originalCap = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), capGep);
    highVal = stmt.contains("high") && !stmt["high"].is_null()
                  ? evaluateExpression(ctx, stmt["high"])
                  : ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
  } else {
    ctx.Error.fatal("slice_read: unrecognized container kind '" + std::string(leftKind) + "'", stmt);
    return;
  }

  llvm::Value *newLen = ctx.Builder->CreateSub(highVal, lowVal, "slice_len");
  llvm::Value *newCap = ctx.Builder->CreateSub(originalCap, lowVal, "slice_cap");
  llvm::Type *baseTy = llvmTypeFor(ctx, stmt["container_type"]["elem_type"]);
  llvm::Value *newDataPtr = ctx.Builder->CreateGEP(baseTy, originalDataPtr, lowVal, "slice_data_ptr");

  llvm::Type *sliceTy = llvmTypeFor(ctx, stmt["result_type"]);

  // Write values into the destination slice struct sitting on the stack frame
  ctx.Builder->CreateStore(
      originalRawPtr, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}));
  ctx.Builder->CreateStore(
      newDataPtr, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}));
  ctx.Builder->CreateStore(
      newLen, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(2)}));
  ctx.Builder->CreateStore(
      newCap, ctx.Builder->CreateGEP(sliceTy, dstPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(3)}));

  if (leftKind == "vector" || leftKind == "string" || leftKind == "array" || leftKind == "view") {
    llvm::FunctionCallee retainFn = ctx.Module->getOrInsertFunction(
        "maml_retain", llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                               {llvm::PointerType::getUnqual(ctx.Context)}, false));
    ctx.Builder->CreateCall(retainFn, {originalRawPtr});
  }
}

static void handleIndexRead(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  llvm::Value *leftVal = evaluateExpression(ctx, stmt["source"]);
  llvm::Value *indexVal = evaluateExpression(ctx, stmt["index"]);
  llvm::Type *elemTy = llvmTypeFor(ctx, stmt["type"]);

  llvm::Value *leftPtr = leftVal;
  if (stmt["source"].contains("value")) {
    std::string leftName = stmt["source"]["value"].get<std::string>();
    if (llvm::Value *sym = ctx.resolveSymbol(leftName)) {
      if (llvm::isa<llvm::AllocaInst>(sym)) leftPtr = sym;
    }
  }

  const auto &leftTypeJson = stmt["source_type"];
  std::string_view kind = leftTypeJson.is_string() ? leftTypeJson.get<std::string_view>()
                                                   : (leftTypeJson.is_object() && leftTypeJson.contains("kind")
                                                          ? leftTypeJson["kind"].get<std::string_view>()
                                                          : "unknown");

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
  } else if (kind == "vector") {
    // Vector is an opaque handle pointer. Load the handle out of memory.
    llvm::Value *vecHandle = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), leftPtr, "vec_handle");

    // Call the runtime lookup API directly to enforce bounds safety
    llvm::FunctionCallee getFn = ctx.Module->getOrInsertFunction(
        "maml_vec_get", llvm::FunctionType::get(
                            llvm::PointerType::getUnqual(ctx.Context),
                            {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)}, false));
    elemPtr = ctx.Builder->CreateCall(getFn, {vecHandle, indexVal}, "vec_get_ptr");
  } else if (kind == "view") {
    // View is a stack-allocated struct value. Access field 1 (data_ptr) locally.
    llvm::Type *hdrTy = llvmTypeFor(ctx, leftTypeJson);
    llvm::Value *dataPtrGep =
        ctx.Builder->CreateGEP(hdrTy, leftPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}, "slice_data_gep");
    llvm::Value *dataPtr =
        ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "slice_data_ptr");
    elemPtr = ctx.Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_elem_ptr");
  } else {
    ctx.Error.fatal("index_read: unrecognised container kind '" + std::string(kind) + "'", stmt);
    return;
  }

  llvm::Value *loadedVal = nullptr;
  if (kind == "string") {
    llvm::Value *charVal = ctx.Builder->CreateLoad(llvm::Type::getInt8Ty(ctx.Context), elemPtr, "char_load");
    loadedVal = ctx.Builder->CreateZExt(charVal, llvm::Type::getInt32Ty(ctx.Context), "char_ext");
  } else {
    loadedVal = ctx.Builder->CreateLoad(elemTy, elemPtr, "elem_load");
  }
  ctx.SymbolEnv.back()[dst] = loadedVal;
}

static void handleIndexAssign(CodegenContext &ctx, const nlohmann::json &stmt) {
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
  std::string_view kind = (targetTypeJson.is_object() && targetTypeJson.contains("kind"))
                              ? targetTypeJson["kind"].get<std::string_view>()
                              : "unknown";

  llvm::Value *indexVal = evaluateExpression(ctx, stmt["index"]);
  llvm::Value *storeVal = evaluateExpression(ctx, stmt["value"]);
  llvm::Value *elemPtr = nullptr;

  if (kind == "array") {
    llvm::Type *arrayTy = llvmTypeFor(ctx, targetTypeJson);
    elemPtr = ctx.Builder->CreateGEP(arrayTy, arrayPtr, {ctx.Builder->getInt32(0), indexVal}, "index_assign_ptr");
  } else if (kind == "vector") {
    // Vector is an opaque handle pointer. Load the handle out of memory.
    llvm::Value *vecHandle = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), arrayPtr, "vec_handle");

    // Spill item value to entry block stack to pass by pointer reference to runtime
    llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
    llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
    llvm::AllocaInst *itemSpill = TmpBuilder.CreateAlloca(storeVal->getType(), nullptr, "vec_assign_spill");
    ctx.Builder->CreateStore(storeVal, itemSpill);

    // Delegate mutation to the runtime helper
    llvm::FunctionCallee setFn = ctx.Module->getOrInsertFunction(
        "maml_vec_set",
        llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context),
                                 llvm::PointerType::getUnqual(ctx.Context)},
                                false));
    ctx.Builder->CreateCall(setFn, {vecHandle, indexVal, itemSpill});
  } else if (kind == "view") {
    // View is a stack struct value. Read field 1 (data_ptr) and inline-store directly.
    llvm::Type *hdrTy = llvmTypeFor(ctx, targetTypeJson);
    llvm::Value *dataPtrGep =
        ctx.Builder->CreateGEP(hdrTy, arrayPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)}, "slice_data_gep");
    llvm::Value *dataPtr =
        ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtrGep, "slice_data_ptr");
    llvm::Type *elemTy = storeVal->getType();
    elemPtr = ctx.Builder->CreateGEP(elemTy, dataPtr, indexVal, "slice_assign_ptr");
  } else {
    ctx.Error.fatal("index_assign: unrecognised container kind '" + std::string(kind) + "'", stmt);
    return;
  }

  if (elemPtr) {
    ctx.Builder->CreateStore(storeVal, elemPtr);
  }
}

// -----------------------------------------------------------------------------
// Public Cluster Entry Point
// -----------------------------------------------------------------------------

void compileContainerOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt) {
  switch (op) {
    case MirOp::ArrayInit:
      return handleArrayInit(ctx, stmt);
    case MirOp::SliceRead:
      return handleSliceRead(ctx, stmt);
    case MirOp::IndexRead:
      return handleIndexRead(ctx, stmt);
    case MirOp::IndexAssign:
      return handleIndexAssign(ctx, stmt);
    default:
      break;
  }
}

}  // namespace maml