#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"

namespace maml {

void handle(CodegenContext &ctx, const mir::StructInitInst &inst) {
  llvm::Value *dstPtr = ctx.getMemoryBase(inst.dst);
  llvm::Type *structTy = ctx.SymbolTypes[inst.dst];
  if (!dstPtr || !structTy) {
    ctx.Error.fatal("struct_init: target memory or type not found for '" + inst.dst + "'");
    return;
  }

  llvm::Value *val = evaluateValue(ctx, inst.value);
  llvm::Value *fieldGep = ctx.Builder->CreateStructGEP(structTy, dstPtr, inst.field_index, inst.dst + "_init_gep");
  ctx.Builder->CreateStore(val, fieldGep);
}

void handle(CodegenContext &ctx, const mir::VariantInitInst &inst) {
  llvm::Value *dstPtr = ctx.getMemoryBase(inst.dst);
  if (!dstPtr) return;

  llvm::Type *sumTy = llvmTypeFor(ctx, inst.type);  // Or ctx.SymbolTypes[inst.dst]
  llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(sumTy, dstPtr, 0, inst.dst + "_disc_gep");
  llvm::Value *discrimVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), inst.discriminant);
  ctx.Builder->CreateStore(discrimVal, discrimGep);

  if (inst.payloads.size() > 0) {
    llvm::Value *arrayGep = ctx.Builder->CreateStructGEP(sumTy, dstPtr, 1, inst.dst + "_array_gep");
    for (size_t i = 0; i < inst.payloads.size(); ++i) {
      llvm::Value *payloadVal = evaluateValue(ctx, inst.payloads[i]);
      llvm::Type *payloadTy = payloadVal->getType();
      llvm::Value *castGep = ctx.Builder->CreateBitCast(arrayGep, llvm::PointerType::getUnqual(payloadTy));
      if (i > 0) {
        castGep = ctx.Builder->CreateGEP(payloadTy, castGep, llvm::ConstantInt::get(ctx.Context, llvm::APInt(32, i)));
      }
      ctx.Builder->CreateStore(payloadVal, castGep);
    }
  }
}

void handle(CodegenContext &ctx, const mir::FieldReadInst &inst) {
  llvm::Value *objVal = evaluateValue(ctx, inst.object);
  if (!objVal) {
    ctx.Error.fatal("field_read: Failed to evaluate object expression");
    return;
  }

  llvm::Value *objPtr = nullptr;
  llvm::Type *structTy = nullptr;

  if (objVal->getType()->isPointerTy()) {
    objPtr = objVal;
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(objVal)) {
      structTy = alloca->getAllocatedType();
    } else if (auto *reg = std::get_if<mir::Register>(&inst.object.inner)) {
      // FIX: Extract the true parent struct layout from the object's register type,
      // instead of using the instruction's type (which belongs to the extracted field).
      structTy = llvmTypeFor(ctx, reg->type);
    } else {
      ctx.Error.fatal("field_read: Unable to deduce parent struct layout for opaque pointer.");
      return;
    }
  } else if (objVal->getType()->isStructTy()) {
    llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
    llvm::IRBuilder<> TmpBuilder(&F->getEntryBlock(), F->getEntryBlock().begin());
    llvm::AllocaInst *spillAlloca = TmpBuilder.CreateAlloca(objVal->getType(), nullptr, "field_read_spill");
    ctx.Builder->CreateStore(objVal, spillAlloca);
    objPtr = spillAlloca;
    structTy = objVal->getType();
  } else {
    ctx.Error.fatal("field_read: Invalid object pointer");
    return;
  }

  llvm::Value *fieldGep = ctx.Builder->CreateStructGEP(structTy, objPtr, inst.field_index, inst.dst + "_gep");
  llvm::Type *fieldTy = llvmTypeFor(ctx, inst.type);
  ctx.SymbolEnv.back()[inst.dst] = ctx.Builder->CreateLoad(fieldTy, fieldGep, inst.dst + "_val");
}

void handle(CodegenContext &ctx, const mir::VariantDiscriminantInst &inst) {
  llvm::Value *objectPtr = evaluateValue(ctx, inst.object);
  llvm::Value *discrimVal = nullptr;

  if (objectPtr->getType()->isPointerTy()) {
    llvm::StructType *sumTy = llvm::StructType::get(ctx.Context, {llvm::Type::getInt32Ty(ctx.Context)}, false);
    llvm::Value *discrimGep = ctx.Builder->CreateStructGEP(sumTy, objectPtr, 0, inst.dst + "_gep");
    discrimVal = ctx.Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), discrimGep, inst.dst + "_val");
  } else {
    discrimVal = ctx.Builder->CreateExtractValue(objectPtr, {0}, inst.dst + "_val");
  }
  ctx.SymbolEnv.back()[inst.dst] = discrimVal;
}

void handle(CodegenContext &ctx, const mir::VariantReadInst &inst) {
  llvm::Value *objectVal = evaluateValue(ctx, inst.object);
  llvm::Value *objectPtr = objectVal;
  llvm::Type *sumTy = nullptr;

  // 1. Spill to stack if it's a value, ensuring we have a pointer to GEP into
  if (!objectVal->getType()->isPointerTy()) {
    llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
    llvm::IRBuilder<> TmpBuilder(&F->getEntryBlock(), F->getEntryBlock().begin());
    llvm::AllocaInst *spillAlloca = TmpBuilder.CreateAlloca(objectVal->getType(), nullptr, "variant_read_spill");
    ctx.Builder->CreateStore(objectVal, spillAlloca);

    objectPtr = spillAlloca;
    sumTy = objectVal->getType();
  } else {
    // If it's already a pointer, grab the allocation type
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(objectVal)) {
      sumTy = alloca->getAllocatedType();
    } else {
      ctx.Error.fatal("variant_read: Unexpected opaque pointer without backing alloca.");
      return;
    }
  }

  // 2. GEP to the payload array (index 1 of the SumType)
  llvm::Value *arrayGep = ctx.Builder->CreateStructGEP(sumTy, objectPtr, 1, inst.dst + "_array_gep");

  // 3. Dynamically reconstruct the variant's payload StructType from the object's type schema
  std::vector<llvm::Type *> payloadTypes;

  if (auto *reg = std::get_if<mir::Register>(&inst.object.inner)) {
    if (auto *sumStruct = std::get_if<maml::SumType>(&reg->type->inner)) {
      for (const auto &varDef : sumStruct->variants) {
        if (varDef.name == inst.variant_name) {
          for (const auto &tt : varDef.tuple_types) {
            payloadTypes.push_back(llvmTypeFor(ctx, tt));
          }
          for (const auto &f : varDef.fields) {
            payloadTypes.push_back(llvmTypeFor(ctx, f.type));
          }
          break;
        }
      }
    }
  }

  // Fallback just in case schema data was dropped for a single-item payload
  if (payloadTypes.empty()) {
    payloadTypes.push_back(llvmTypeFor(ctx, inst.type));
  }

  llvm::StructType *variantStructTy = llvm::StructType::get(ctx.Context, payloadTypes, false);

  // 4. Cast the array pointer to the dynamically generated struct pointer
  llvm::Value *structPtr =
      ctx.Builder->CreateBitCast(arrayGep, llvm::PointerType::getUnqual(variantStructTy), "variant_cast");

  // 5. Apply LLVM's intelligent StructGEP to resolve the exact byte offset for the field
  llvm::Value *castGep =
      ctx.Builder->CreateStructGEP(variantStructTy, structPtr, inst.payload_index, inst.dst + "_payload_gep");
  llvm::Type *targetTy = llvmTypeFor(ctx, inst.type);

  // 6. Safely load the properly-aligned payload
  llvm::Value *loadedVal = ctx.Builder->CreateLoad(targetTy, castGep, inst.dst + "_val");
  ctx.SymbolEnv.back()[inst.dst] = loadedVal;
}

}  // namespace maml