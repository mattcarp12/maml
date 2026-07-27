#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"
#include "mir.h"

namespace maml {

void handle(CodegenContext& ctx, const mir::AllocaInst& inst)
{
    llvm::Type* llvmTy = llvmTypeFor(ctx, inst.type);
    if (!llvmTy)
        return;

    // Allocate on the stack and register it in the environment
    llvm::AllocaInst* alloca
        = ctx.Builder->CreateAlloca(llvmTy, nullptr, ctx.Sym.resolve(inst.dst));
    ctx.SymbolEnv.back()[inst.dst] = alloca;
    ctx.SymbolTypes[inst.dst] = llvmTy;
}

void handle(CodegenContext& ctx, const mir::AddressOfInst& inst)
{
    llvm::Value* srcSlot = ctx.resolveSymbol(inst.src);
    if (!srcSlot) {
        ctx.Error.fatal("address_of: undefined variable " + std::string(ctx.Sym.resolve(inst.src)));
        return;
    }
    llvm::Value* dstSlot = ctx.resolveSymbol(inst.dst);
    ctx.Builder->CreateStore(srcSlot, dstSlot);
}

void handle(CodegenContext& ctx, const mir::AssignInst& inst)
{
    if (inst.dst == NoSymbol || ctx.Sym.resolve(inst.dst) == "_")
        return;
    llvm::Type* dstTy = ctx.SymbolTypes[inst.dst];
    if (dstTy && dstTy->isVoidTy())
        return;

    llvm::Value* val = evaluateValue(ctx, inst.rValue);
    llvm::Value* target = ctx.getMemoryBase(inst.dst);
    if (!target || !val || val->getType()->isVoidTy())
        return;
    ctx.Builder->CreateStore(val, target);
}

void handle(CodegenContext& ctx, const mir::CopyInst& inst)
{
    llvm::Value* dstPtr = ctx.getMemoryBase(inst.dst);
    llvm::Value* srcVal = ctx.getMemoryBase(inst.src);
    llvm::Value* rawSym = ctx.resolveSymbol(inst.src);
    bool srcIsMemory = rawSym->getType()->isPointerTy();

    std::string srcName = std::string(ctx.Sym.resolve(inst.src));

    if (srcIsMemory) {
        llvm::Type* srcTy = ctx.SymbolTypes[inst.src];
        llvm::Type* dstTy = ctx.SymbolTypes[inst.dst];

        if (srcTy && srcTy->isPointerTy() && dstTy && !dstTy->isPointerTy()) {
            llvm::Value* derefPtr = ctx.Builder->CreateLoad(srcTy, srcVal, srcName + "_deref");
            llvm::Value* loaded = ctx.Builder->CreateLoad(dstTy, derefPtr, srcName + "_val");
            ctx.Builder->CreateStore(loaded, dstPtr);
        } else {
            llvm::Value* loaded = ctx.Builder->CreateLoad(srcTy, srcVal, srcName + "_copy_load");
            ctx.Builder->CreateStore(loaded, dstPtr);
        }
    } else {
        ctx.Builder->CreateStore(srcVal, dstPtr);
    }
}

void handle(CodegenContext& ctx, const mir::MoveInst& inst)
{
    llvm::Value* dstPtr = ctx.getMemoryBase(inst.dst);
    llvm::Value* srcVal = ctx.getMemoryBase(inst.src);
    if (!dstPtr || !srcVal)
        return;

    llvm::Value* rawSym = ctx.resolveSymbol(inst.src);
    bool srcIsMemory = rawSym->getType()->isPointerTy();

    if (srcIsMemory) {
        llvm::Type* ty = ctx.SymbolTypes[inst.src];
        std::string srcName = std::string(ctx.Sym.resolve(inst.src));
        llvm::Value* loaded = ctx.Builder->CreateLoad(ty, srcVal, srcName + "_move_load");
        ctx.Builder->CreateStore(loaded, dstPtr);
    } else {
        ctx.Builder->CreateStore(srcVal, dstPtr);
    }
}

void handle(CodegenContext& ctx, const mir::LoadPtrInst& inst)
{
    llvm::Value* ptrVal = evaluateValue(ctx, inst.ptr);
    llvm::Type* targetTy = llvmTypeFor(ctx, inst.type);
    std::string dstName = std::string(ctx.Sym.resolve(inst.dst));
    llvm::Value* loadedVal = ctx.Builder->CreateLoad(targetTy, ptrVal, dstName + "_load");
    llvm::Value* dstSlot = ctx.getMemoryBase(inst.dst); // the pre-allocated alloca for inst.dst
    ctx.Builder->CreateStore(loadedVal, dstSlot);
}

void handle(CodegenContext& ctx, const mir::StoreInst& inst)
{
    llvm::Value* val = evaluateValue(ctx, inst.value);
    llvm::Value* dstPtr = evaluateValue(ctx, inst.dstPtr);
    if (!dstPtr) {
        ctx.Error.fatal("store: failed to evaluate destination pointer");
        return;
    }
    if (dstPtr->getType()->isPointerTy()) {
        ctx.Builder->CreateStore(val, dstPtr);
    }
}

void handle(CodegenContext& ctx, const mir::CastInst& inst)
{
    llvm::Value* srcVal = evaluateValue(ctx, inst.src);
    llvm::Type* targetTy = llvmTypeFor(ctx, inst.type);
    std::string dstName = std::string(ctx.Sym.resolve(inst.dst));
    llvm::Value* castVal = nullptr;

    if (srcVal->getType()->isIntegerTy() && targetTy->isIntegerTy()) {
        castVal = ctx.Builder->CreateZExtOrTrunc(srcVal, targetTy, dstName + "_cast");
    } else if (srcVal->getType()->isPointerTy() && targetTy->isPointerTy()) {
        castVal = ctx.Builder->CreatePointerCast(srcVal, targetTy, dstName + "_cast");
    } else if (srcVal->getType()->isIntegerTy() && targetTy->isPointerTy()) {
        castVal = ctx.Builder->CreateIntToPtr(srcVal, targetTy, dstName + "_cast");
    } else if (srcVal->getType()->isPointerTy() && targetTy->isIntegerTy()) {
        castVal = ctx.Builder->CreatePtrToInt(srcVal, targetTy, dstName + "_cast");
    } else {
        ctx.Error.fatal("cast: unsupported cast operation");
        return;
    }
    ctx.SymbolEnv.back()[inst.dst] = castVal;
}

void handle(CodegenContext& ctx, const mir::BitcastPtrInst& inst)
{
    llvm::Value* srcPtr = evaluateValue(ctx, inst.src);
    ctx.SymbolEnv.back()[inst.dst] = srcPtr;
}

void handle(CodegenContext& ctx, const mir::CoroPrologueInst& inst)
{
    (void)inst;
    auto& Builder = ctx.Builder;
    auto& Context = ctx.Context;
    auto* Module = ctx.Module.get();

    // 1. Allocate a generic 32-byte Promise slot to hold the return value
    llvm::Type* promiseTy = llvm::ArrayType::get(llvm::Type::getInt64Ty(Context), 4);
    ctx.PromiseSlot = Builder->CreateAlloca(promiseTy, nullptr, "promise");

    // 2. Pass the Promise into coro.id so LLVM tracks its offset in the frame
    llvm::Function* coroIdFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_id);
    llvm::Value* nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));
    llvm::Value* promisePtr
        = Builder->CreatePointerCast(ctx.PromiseSlot, llvm::PointerType::getUnqual(Context));

    ctx.CoroId = Builder->CreateCall(coroIdFn,
        { llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0), promisePtr, nullPtr,
            nullPtr },
        "coro.id");

    // 3. Size and Alloc
    llvm::Function* coroSizeFn = llvm::Intrinsic::getDeclaration(
        Module, llvm::Intrinsic::coro_size, { llvm::Type::getInt64Ty(Context) });
    llvm::Value* coroSize = Builder->CreateCall(coroSizeFn, {}, "coro.size");
    llvm::Function* allocFn = Module->getFunction("maml_alloc");
    llvm::Value* framePtr = Builder->CreateCall(allocFn, { coroSize }, "coro.frame.alloc");

    // 4. Begin
    llvm::Function* coroBeginFn
        = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_begin);
    ctx.CurrentCoroHandle
        = Builder->CreateCall(coroBeginFn, { ctx.CoroId, framePtr }, "coro.handle");

    if (llvm::Value* handleAlloca = ctx.resolveSymbol(ctx.Sym.intern("__coro_handle"))) {
        ctx.Builder->CreateStore(ctx.CurrentCoroHandle, handleAlloca);
    }

    // Initial suspend
    llvm::Function* coroSaveFn
        = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_save);
    llvm::Value* initSaveToken
        = Builder->CreateCall(coroSaveFn, { ctx.CurrentCoroHandle }, "init.save");
    llvm::Function* coroSuspendFn
        = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_suspend);
    llvm::Value* suspendResult = Builder->CreateCall(
        coroSuspendFn, { initSaveToken, Builder->getInt1(false) }, "init.suspend");

    // Route: resume → bb0 (the actual MIR entry block), destroy → cleanup
    // NOTE: no userCodeBB needed — bb0 IS the user code block
    llvm::BasicBlock* mirEntryBB = ctx.Blocks[ctx.CoroEntryBlockId];
    llvm::SwitchInst* sw = Builder->CreateSwitch(suspendResult, ctx.CoroSuspendBlock, 2);
    sw->addCase(Builder->getInt8(0), mirEntryBB);
    sw->addCase(Builder->getInt8(1), ctx.CoroCleanupBlock);
}

void handle(CodegenContext& ctx, const mir::CoroPromisePtrInst& inst)
{
    llvm::Value* hdl = evaluateValue(ctx, inst.handle);

    llvm::Function* promiseFn
        = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_promise);
    llvm::Value* align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 8);
    llvm::Value* from = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);

    std::string dstName = std::string(ctx.Sym.resolve(inst.dst));
    llvm::Value* promisePtr
        = ctx.Builder->CreateCall(promiseFn, { hdl, align, from }, dstName + "_promise");

    ctx.SymbolEnv.back()[inst.dst] = promisePtr;
}

} // namespace maml