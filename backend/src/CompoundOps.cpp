#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"
#include "mir.h"

namespace maml {

void handle(CodegenContext& ctx, const mir::FieldAddrInst& inst)
{
    llvm::Value* objPtr = evaluateAddress(ctx, inst.object);
    if (!objPtr || !objPtr->getType()->isPointerTy()) {
        ctx.Error.fatal("field_addr: Expected a pointer base for address calculation");
        return;
    }
    llvm::Type* structLayoutTy = llvmLayoutTypeFor(ctx, inst.objectType);
    if (!structLayoutTy || structLayoutTy->isPointerTy()) {
        ctx.Error.fatal("field_addr: Layout resolved to a pointer instead of a structural type!");
        return;
    }
    std::string dstName = std::string(ctx.Sym.resolve(inst.dst));
    llvm::Value* fieldGep
        = ctx.Builder->CreateStructGEP(structLayoutTy, objPtr, inst.fieldIndex, dstName + "_gep");
    ctx.SymbolEnv.back()[inst.dst] = fieldGep;
}

void handle(CodegenContext& ctx, const mir::IndexAddrInst& inst)
{
    llvm::Value* basePtr = evaluateAddress(ctx, inst.source);
    llvm::Type* baseTy = llvmLayoutTypeFor(ctx, inst.sourceType);
    llvm::Value* idxVal = evaluateValue(ctx, inst.index);
    if (!basePtr || !basePtr->getType()->isPointerTy()) {
        ctx.Error.fatal("index_addr: Expected a pointer base for address calculation");
        return;
    }
    std::string dstName = std::string(ctx.Sym.resolve(inst.dst));
    llvm::Value* elementGep = nullptr;
    if (baseTy->isArrayTy()) {
        llvm::Value* zero = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
        elementGep
            = ctx.Builder->CreateInBoundsGEP(baseTy, basePtr, { zero, idxVal }, dstName + "_gep");
    } else {
        llvm::Type* elemTy = llvmTypeFor(ctx, inst.type);
        elementGep = ctx.Builder->CreateInBoundsGEP(elemTy, basePtr, idxVal, dstName + "_gep");
    }
    ctx.SymbolEnv.back()[inst.dst] = elementGep;
}

} // namespace maml