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

  llvm::Value *objectPtr = evaluateExpression(ctx, objectJson);
  if (!objectPtr) {
    ctx.Error.fatal("Failed to evaluate object pointer context for variant_discriminant", stmt);
    return;
  }

  // Resolve the underlying tag structural layout of the base SumType
  llvm::Type *sumTy = llvmTypeFor(ctx, objectJson["type"]);

  // The tag discriminant is structurally guaranteed to always reside at layout index 0
  llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(sumTy, objectPtr, 0, dst + "_gep");
  llvm::Value *discrimVal = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), discrimGep, dst + "_val");

  ctx.SymbolEnv.back()[dst] = discrimVal;
}

static void handleVariantRead(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt.value("dst", "");
  std::string variant_name = stmt.value("variant_name", "");
  int payload_index = stmt.value("payload_index", 0);
  auto objectJson = stmt["object"];

  llvm::Value *objectPtr = evaluateExpression(ctx, objectJson);
  if (!objectPtr) {
    ctx.Error.fatal("Failed to evaluate target object pointer for variant_read", stmt);
    return;
  }

  // Reconstruct the structural field layout schema for this specific algebraic variant branch
  std::vector<llvm::Type *> fieldTys;
  fieldTys.push_back(llvm::Type::getInt32Ty(ctx.Context));  // Index 0 is always the variant tag discriminant

  if (objectJson.contains("type") && objectJson["type"].contains("variants")) {
    for (const auto &v : objectJson["type"]["variants"]) {
      if (v.value("name", "") == variant_name) {
        if (v.contains("tuple_types")) {
          for (const auto &tJson : v["tuple_types"]) fieldTys.push_back(llvmTypeFor(ctx, tJson));
        }
        if (v.contains("fields")) {
          for (const auto &fJson : v["fields"]) fieldTys.push_back(llvmTypeFor(ctx, fJson["type"]));
        }
        break;
      }
    }
  }

  // Calibration Failsafe: Backfill sparse definitions up to target index boundaries
  if (fieldTys.size() <= static_cast<size_t>(1 + payload_index)) {
    while (fieldTys.size() <= static_cast<size_t>(1 + payload_index)) {
      fieldTys.push_back(llvmTypeFor(ctx, stmt["type"]));
    }
  }

  llvm::StructType *variantStructTy = llvm::StructType::get(ctx.Context, fieldTys, /*isPacked=*/false);

  // Step past index 0 discriminant to extract payload at position (1 + payload_index)
  llvm::Value *payloadGep = ctx.Builder->CreateStructGEP(variantStructTy, objectPtr, 1 + payload_index, dst + "_gep");
  llvm::Type *targetTy = llvmTypeFor(ctx, stmt["type"]);
  llvm::Value *loadedVal = ctx.Builder->CreateLoad(targetTy, payloadGep, dst + "_val");

  ctx.SymbolEnv.back()[dst] = loadedVal;
}

static void handleVariantInit(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt.value("dst", "");
  int discriminant = stmt.value("discriminant", 0);
  auto payloadsJson = stmt["payloads"];

  llvm::Value *dstPtr = ctx.resolveSymbol(dst);
  if (!dstPtr) {
    ctx.Error.fatal("variant_init: target memory allocation pointer not found for '" + dst + "'", stmt);
    return;
  }

  // Reconstruct the anonymous tuple layout structure for writing fields
  std::vector<llvm::Type *> fieldTys;
  fieldTys.push_back(llvm::Type::getInt32Ty(ctx.Context));  // Discriminant tag
  for (const auto &pJson : payloadsJson) {
    fieldTys.push_back(llvmTypeFor(ctx, pJson["type"]));
  }

  llvm::StructType *variantStructTy = llvm::StructType::get(ctx.Context, fieldTys, /*isPacked=*/false);

  // Write the tag value into index 0
  llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(variantStructTy, dstPtr, 0, dst + "_disc_gep");
  llvm::Value *discrimVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), discriminant);
  ctx.Builder->CreateStore(discrimVal, discrimGep);

  // Pack each field value down sequentially into the variant allocation buffer
  for (size_t i = 0; i < payloadsJson.size(); ++i) {
    auto pJson = payloadsJson[i];
    llvm::Value *payloadVal = evaluateExpression(ctx, pJson);
    if (!payloadVal) {
      ctx.Error.fatal("variant_init: Failed to evaluate payload field parameter index " + std::to_string(i), stmt);
      continue;
    }

    llvm::Value *payloadGep =
        ctx.Builder->CreateStructGEP(variantStructTy, dstPtr, 1 + i, dst + "_pld_" + std::to_string(i) + "_gep");
    ctx.Builder->CreateStore(payloadVal, payloadGep);
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