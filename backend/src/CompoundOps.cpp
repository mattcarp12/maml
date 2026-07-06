#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"
#include "mir_generated.hpp"

namespace maml {

void handle(CodegenContext &ctx, const mir::FieldAddrInst &inst) {
  llvm::Value *objPtr = evaluateAddress(ctx, inst.object);
  if (!objPtr || !objPtr->getType()->isPointerTy()) {
    ctx.Error.fatal("field_addr: Expected a pointer base for address calculation");
    return;
  }
  llvm::Type *structLayoutTy = llvmLayoutTypeFor(ctx, inst.object_type);
  if (!structLayoutTy || structLayoutTy->isPointerTy()) {
    ctx.Error.fatal("field_addr: Layout resolved to a pointer instead of a structural type!");
    return;
  }
  llvm::Value *fieldGep = ctx.Builder->CreateStructGEP(structLayoutTy, objPtr, inst.field_index, inst.dst + "_gep");
  ctx.SymbolEnv.back()[inst.dst] = fieldGep;
}

void handle(CodegenContext &ctx, const mir::IndexAddrInst &inst) {
  llvm::Value *basePtr = evaluateAddress(ctx, inst.source);
  llvm::Type *baseTy = llvmLayoutTypeFor(ctx, inst.source_type);
  llvm::Value *idxVal = evaluateValue(ctx, inst.index);
  if (!basePtr || !basePtr->getType()->isPointerTy()) {
    ctx.Error.fatal("index_addr: Expected a pointer base for address calculation");
    return;
  }
  llvm::Value *elementGep = nullptr;
  if (baseTy->isArrayTy()) {
    llvm::Value *zero = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
    elementGep = ctx.Builder->CreateInBoundsGEP(baseTy, basePtr, {zero, idxVal}, inst.dst + "_gep");
  } else {
    llvm::Type *elemTy = llvmTypeFor(ctx, inst.type);
    elementGep = ctx.Builder->CreateInBoundsGEP(elemTy, basePtr, idxVal, inst.dst + "_gep");
  }
  ctx.SymbolEnv.back()[inst.dst] = elementGep;
}

}  // namespace maml