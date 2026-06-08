#include <llvm/IR/DataLayout.h>

#include <string>
#include <vector>

#include "ExprGenerator.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

// -----------------------------------------------------------------------------
// Private, Isolated Instruction Helpers
// -----------------------------------------------------------------------------

static void handleStructInit(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt.value("dst", "");
  int field_index = stmt.value("field_index", 0);

  llvm::Value *dstPtr = ctx.resolveSymbol(dst);
  if (!dstPtr) {
    ctx.Error.fatal("struct_init: target stack allocation not found for '" + dst + "'", stmt);
    return;
  }

  // Derive structural type layout safely from the stack alloca allocation
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
}

static void handleFieldRead(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt.value("dst", "");
  int field_index = stmt.value("field_index", 0);
  auto objectJson = stmt["object"];

  // 🌟 THE FIX: Evaluate the expression directly instead of looking up a raw symbol string
  llvm::Value *objVal = evaluateExpression(ctx, objectJson);
  if (!objVal) {
    ctx.Error.fatal("field_read: Failed to evaluate object expression", stmt);
    return;
  }

  llvm::Value *objPtr = nullptr;
  llvm::Type *structTy = nullptr;

  // Case A: The evaluated object is an active memory pointer (like a stack Alloca)
  if (objVal->getType()->isPointerTy()) {
    objPtr = objVal;

    // If it's an alloca, we can safely pull its underlying allocated type
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(objVal)) {
      structTy = alloca->getAllocatedType();
    } else {
      // If it's an opaque pointer, lower the frontend's object type definition to map the layout
      structTy = llvmTypeFor(ctx, objectJson["type"]);
    }
  }
  // Case B: The evaluated object is a direct, loaded struct register (like an inline literal or nested read)
  else if (objVal->getType()->isStructTy()) {
    // Spill the raw structural register back to an entry-block stack temporary to grab a memory address
    llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
    llvm::IRBuilder<> TmpBuilder(&F->getEntryBlock(), F->getEntryBlock().begin());
    llvm::AllocaInst *spillAlloca = TmpBuilder.CreateAlloca(objVal->getType(), nullptr, "field_read_source_spill");

    ctx.Builder->CreateStore(objVal, spillAlloca);
    objPtr = spillAlloca;
    structTy = objVal->getType();
  } else {
    ctx.Error.fatal("field_read: Target object expression must resolve to a pointer or structural layout value", stmt);
    return;
  }

  // Perform a clean, type-safe Structural GEP + Load
  llvm::Value *fieldGep = ctx.Builder->CreateStructGEP(structTy, objPtr, field_index, dst + "_gep");
  llvm::Type *fieldTy = llvmTypeFor(ctx, stmt["type"]);
  llvm::Value *loadedVal = ctx.Builder->CreateLoad(fieldTy, fieldGep, dst + "_val");

  ctx.SymbolEnv.back()[dst] = loadedVal;
}

static void handleVariantDiscriminant(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  auto objectJson = stmt["object"];

  // 🌟 THE FIX: Grab the memory pointer directly to ensure GEP access
  llvm::Value *objectPtr = nullptr;
  if (objectJson.contains("op") && objectJson["op"] == "ident") {
    objectPtr = ctx.resolveSymbol(objectJson["value"].get<std::string>());
  }

  if (!objectPtr) {
    objectPtr = evaluateExpression(ctx, objectJson);
  }

  llvm::Value *discrimVal = nullptr;

  if (objectPtr->getType()->isPointerTy()) {
    // We only need the tag, so a simple { i32 } struct overlay guarantees we read offset 0 correctly
    llvm::StructType *sumTy = llvm::StructType::get(ctx.Context, {llvm::Type::getInt32Ty(ctx.Context)}, false);
    llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(sumTy, objectPtr, 0, dst + "_gep");
    discrimVal = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), discrimGep, dst + "_val");
  } else {
    // Fallback
    discrimVal = ctx.Builder->CreateExtractValue(objectPtr, {0}, dst + "_val");
  }

  ctx.SymbolEnv.back()[dst] = discrimVal;
}

static void handleVariantRead(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt.value("dst", "");
  int payload_index = stmt.value("payload_index", 0);
  auto objectJson = stmt["object"];

  llvm::Value *objectPtr = nullptr;
  if (objectJson.contains("op") && objectJson["op"] == "ident") {
    objectPtr = ctx.resolveSymbol(objectJson["value"].get<std::string>());
  }
  if (!objectPtr) objectPtr = evaluateExpression(ctx, objectJson);

  llvm::Value *loadedVal = nullptr;

  if (objectPtr->getType()->isPointerTy()) {
    // 🌟 THE FIX: Read directly from the padded array at index 1
    llvm::Type *sumTy = llvmTypeFor(ctx, objectJson["type"]);
    llvm::Value *arrayGep = ctx.Builder->CreateStructGEP(sumTy, objectPtr, 1, dst + "_array_gep");

    llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);
    llvm::Value *castGep = ctx.Builder->CreateBitCast(arrayGep, llvm::PointerType::getUnqual(targetTy));

    if (payload_index > 0) {
      castGep = ctx.Builder->CreateGEP(targetTy, castGep,
                                       llvm::ConstantInt::get(ctx.Context, llvm::APInt(32, payload_index)));
    }

    loadedVal = ctx.Builder->CreateLoad(targetTy, castGep, dst + "_val");
  } else {
    // Fallback
    loadedVal = ctx.Builder->CreateExtractValue(objectPtr, {1}, dst + "_val");
  }

  ctx.SymbolEnv.back()[dst] = loadedVal;
}

static void handleVariantInit(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt.value("dst", "");
  int discriminant = stmt.value("discriminant", 0);
  auto payloadsJson = stmt["payloads"];

  llvm::Value *dstPtr = ctx.resolveSymbol(dst);
  if (!dstPtr) return;

  // 🌟 THE FIX: Get the official, padded LLVM type for the SumType
  llvm::Type *sumTy = llvmTypeFor(ctx, stmt["type"]);

  // 1. Write the integer tag into index 0
  llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(sumTy, dstPtr, 0, dst + "_disc_gep");
  llvm::Value *discrimVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), discriminant);
  ctx.Builder->CreateStore(discrimVal, discrimGep);

  // 2. Write the payload directly into the padded array at index 1
  if (payloadsJson.size() > 0) {
    llvm::Value *arrayGep = ctx.Builder->CreateStructGEP(sumTy, dstPtr, 1, dst + "_array_gep");

    for (size_t i = 0; i < payloadsJson.size(); ++i) {
      auto pJson = payloadsJson[i];
      llvm::Value *payloadVal = evaluateExpression(ctx, pJson);

      // Cast the generic array memory to a pointer of our specific payload type
      llvm::Type *payloadTy = payloadVal->getType();
      llvm::Value *castGep = ctx.Builder->CreateBitCast(arrayGep, llvm::PointerType::getUnqual(payloadTy));

      // If there are multiple tuple payloads, we offset the pointer
      if (i > 0) {
        castGep = ctx.Builder->CreateGEP(payloadTy, castGep, llvm::ConstantInt::get(ctx.Context, llvm::APInt(32, i)));
      }

      ctx.Builder->CreateStore(payloadVal, castGep);
    }
  }
}

// -----------------------------------------------------------------------------
// Public Cluster Entry Point
// -----------------------------------------------------------------------------

void compileCompoundOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt) {
  switch (op) {
    case MirOp::StructInit:
      return handleStructInit(ctx, stmt);
    case MirOp::FieldRead:
      return handleFieldRead(ctx, stmt);
    case MirOp::VariantDiscriminant:
      return handleVariantDiscriminant(ctx, stmt);
    case MirOp::VariantRead:
      return handleVariantRead(ctx, stmt);
    case MirOp::VariantInit:
      return handleVariantInit(ctx, stmt);
    default:
      break;
  }
}

}  // namespace maml