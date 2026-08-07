#include "StmtGenerator.hpp"

#include "CodegenContext.hpp"
#include "ExprGenerator.hpp"
#include "mir.h"
#include "llvm/IR/BasicBlock.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/DerivedTypes.h"
#include "llvm/IR/Function.h"
#include "llvm/IR/Instructions.h"
#include "llvm/IR/Intrinsics.h"
#include "llvm/IR/Type.h"
#include "llvm/IR/Value.h"
#include <type_traits>
#include <variant>

namespace maml {

// Forward declare the handler overloads (implemented in the Ops cpp files)
void handle(CodegenContext& ctx, const mir::AssignInst& inst);
void handle(CodegenContext& ctx, const mir::IndexAddrInst& inst);
void handle(CodegenContext& ctx, const mir::FieldAddrInst& inst);
void handle(CodegenContext& ctx, const mir::AddressOfInst& inst);
void handle(CodegenContext& ctx, const mir::AllocaInst& inst);
void handle(CodegenContext& ctx, const mir::BinaryOpInst& inst);
void handle(CodegenContext& ctx, const mir::BitcastPtrInst& inst);
void handle(CodegenContext& ctx, const mir::UnaryOpInst& inst);
void handle(CodegenContext& ctx, const mir::CallInst& inst);
void handle(CodegenContext& ctx, const mir::CastInst& inst);
void handle(CodegenContext& ctx, const mir::LoadPtrInst& inst);
void handle(CodegenContext& ctx, const mir::StoreInst& inst);
void handle(CodegenContext& ctx, const mir::CopyInst& inst);
void handle(CodegenContext& ctx, const mir::MoveInst& inst);
void handle(CodegenContext& ctx, const mir::CoroPrologueInst& inst);
void handle(CodegenContext& ctx, const mir::CoroPromisePtrInst& inst);

void compileInstruction(CodegenContext& ctx, const mir::Instruction& inst)
{
    // -- ADD DEBUG PRINT --
    // llvm::errs() << "[DEBUG compileInstruction] Line: " << getPosOf(inst).line
    //              << " | Variant Index: " << inst.index() << "\n";

    std::visit(
        [&](auto&& arg) {
            using T = std::decay_t<decltype(arg)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                // Nothing to emit.
            } else if constexpr (std::is_same_v<T, mir::BorrowInst>
                || std::is_same_v<T, mir::KeepAliveInst>) {
                // These must have been eliminated by passes::lowerLinearTypes
                // (BorrowInst -> AddressOfInst, KeepAliveInst dropped) before the
                // MIR reaches the backend. Seeing one here means the pass
                // pipeline ran out of order or was skipped.
                ctx.Error.fatal("Internal error: BorrowInst/KeepAliveInst reached codegen; "
                                "passes::lowerLinearTypes must run before compileProgram.");
            } else {
                handle(ctx, arg);
            }
        },
        inst);
}

void compileTerminator(CodegenContext& ctx, const mir::Terminator& term)
{
    std::visit(
        [&](auto&& arg) {
            using T = std::decay_t<decltype(arg)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                ctx.Error.fatal("Encountered an unset terminator during codegen.");
            } else if constexpr (std::is_same_v<T, mir::ReturnTerminator>) {
                if (ctx.Builder->GetInsertBlock()->getParent()->getReturnType()->isVoidTy()) {
                    ctx.Builder->CreateRetVoid();
                } else {
                    llvm::Value* retVal = evaluateValue(ctx, arg.value);
                    // If we are in 'main', the OS requires an i32 exit code
                    llvm::Function* parentFn = ctx.Builder->GetInsertBlock()->getParent();
                    if (parentFn->getName() == "main" && retVal->getType()->isIntegerTy(64)) {
                        retVal = ctx.Builder->CreateTrunc(
                            retVal, llvm::Type::getInt32Ty(ctx.Context), "exit_code_trunc");
                    }
                    ctx.Builder->CreateRet(retVal);
                }
            } else if constexpr (std::is_same_v<T, mir::JumpTerminator>) {
                if (arg.target == mir::InvalidBlock)
                    ctx.Error.fatal("JumpTerminator is missing a target block ID!");
                ctx.Builder->CreateBr(ctx.Blocks[arg.target]);
            } else if constexpr (std::is_same_v<T, mir::BranchTerminator>) {
                if (arg.trueTarget == mir::InvalidBlock || arg.falseTarget == mir::InvalidBlock)
                    ctx.Error.fatal("BranchTerminator is missing a true/false target block ID!");

                llvm::Value* condVal = evaluateValue(ctx, arg.condition);
                ctx.Builder->CreateCondBr(
                    condVal, ctx.Blocks[arg.trueTarget], ctx.Blocks[arg.falseTarget]);
            } else if constexpr (std::is_same_v<T, mir::UnreachableTerminator>) {
                ctx.Builder->CreateUnreachable();
            } else if constexpr (std::is_same_v<T, mir::CoroSuspendTerminator>) {
                if (arg.resumeBlock == mir::InvalidBlock)
                    ctx.Error.fatal("CoroSuspendTerminator is missing a resume target block ID!");

                auto* Module = ctx.Module.get();
                auto& Context = ctx.Context;

                llvm::BasicBlock* resumeBB = ctx.Blocks[arg.resumeBlock];

                // 1. llvm.coro.save - Captures the current state of the coroutine
                llvm::Function* saveFn
                    = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_save);
                llvm::Value* coroState
                    = ctx.Builder->CreateCall(saveFn, { ctx.CurrentCoroHandle }, "coro.state");

                // 2. llvm.coro.suspend - Suspends execution
                llvm::Function* suspendFn
                    = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_suspend);
                llvm::Value* isFinal = llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0);
                llvm::Value* suspendResult
                    = ctx.Builder->CreateCall(suspendFn, { coroState, isFinal }, "suspend.result");

                // 3. Route -1 (Suspend) to caller, 0 (Resume) to resume block, and 1 (Destroy) to
                // cleanup
                llvm::SwitchInst* sw
                    = ctx.Builder->CreateSwitch(suspendResult, ctx.CoroSuspendBlock, 2);
                sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0), resumeBB);
                sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1),
                    ctx.CoroCleanupBlock);
            } else if constexpr (std::is_same_v<T, mir::CoroYieldTerminator>) {
                ctx.Builder->CreateBr(ctx.CoroSuspendBlock);
            } else if constexpr (std::is_same_v<T, mir::CoroFinalSuspendTerminator>) {
                auto& Context = ctx.Context;
                auto* Module = ctx.Module.get();

                // 1. Store the return value in the Promise slot (if a return value exists)
                llvm::Value* retVal = evaluateValue(ctx, arg.value);
                if (retVal && ctx.PromiseSlot) {
                    llvm::Value* typedPromise = ctx.Builder->CreatePointerCast(
                        ctx.PromiseSlot, llvm::PointerType::getUnqual(ctx.Context));
                    ctx.Builder->CreateStore(retVal, typedPromise);
                }

                // 2. Execute a FINAL Suspend
                llvm::Function* saveFn
                    = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_save);
                llvm::Value* coroState = ctx.Builder->CreateCall(saveFn, { ctx.CurrentCoroHandle });
                llvm::Function* suspendFn
                    = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_suspend);
                llvm::Value* isFinal = llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 1);
                llvm::Value* suspendResult
                    = ctx.Builder->CreateCall(suspendFn, { coroState, isFinal });

                // 3. Route -1 (Suspend) to caller. Resume is UB. Destroy goes to cleanup.
                llvm::BasicBlock* unreachableBB = llvm::BasicBlock::Create(
                    Context, "final.unreachable", ctx.Builder->GetInsertBlock()->getParent());

                llvm::SwitchInst* sw
                    = ctx.Builder->CreateSwitch(suspendResult, ctx.CoroSuspendBlock, 2);
                sw->addCase(
                    llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0), unreachableBB);
                sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1),
                    ctx.CoroCleanupBlock);

                ctx.Builder->SetInsertPoint(unreachableBB);
                ctx.Builder->CreateUnreachable();
            }
        },
        term);
}

} // namespace maml