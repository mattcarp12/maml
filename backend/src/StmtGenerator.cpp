#include "StmtGenerator.hpp"

#include "ExprGenerator.hpp"
#include "mir_generated.hpp"

namespace maml {

// Forward declare the handler overloads (implemented in the Ops cpp files)
void handle(CodegenContext &ctx, const mir::TempDeclInst &inst);
void handle(CodegenContext &ctx, const mir::AssignInst &inst);
void handle(CodegenContext &ctx, const mir::IndexAssignInst &inst);
void handle(CodegenContext &ctx, const mir::StructInitInst &inst);
void handle(CodegenContext &ctx, const mir::FieldReadInst &inst);
void handle(CodegenContext &ctx, const mir::ArrayInitInst &inst);
void handle(CodegenContext &ctx, const mir::SliceInst &inst);
void handle(CodegenContext &ctx, const mir::BinaryOpInst &inst);
void handle(CodegenContext &ctx, const mir::UnaryOpInst &inst);
void handle(CodegenContext &ctx, const mir::IndexReadInst &inst);
void handle(CodegenContext &ctx, const mir::CallInst &inst);
void handle(CodegenContext &ctx, const mir::VariantInitInst &inst);
void handle(CodegenContext &ctx, const mir::VariantReadInst &inst);
void handle(CodegenContext &ctx, const mir::VariantDiscriminantInst &inst);
void handle(CodegenContext &ctx, const mir::CastInst &inst);
void handle(CodegenContext &ctx, const mir::LoadPtrInst &inst);
void handle(CodegenContext &ctx, const mir::StoreInst &inst);
void handle(CodegenContext &ctx, const mir::CopyInst &inst);
void handle(CodegenContext &ctx, const mir::MoveInst &inst);
void handle(CodegenContext &ctx, const mir::RefAllocInst &inst);
void handle(CodegenContext &ctx, const mir::RefIncInst &inst);
void handle(CodegenContext &ctx, const mir::RefDecInst &inst);
void handle(CodegenContext &ctx, const mir::CoroPrologueInst &inst);

void compileInstruction(CodegenContext &ctx, const mir::Instruction &inst) {
  std::visit([&](auto &&arg) { handle(ctx, arg); }, inst.inner);
}

void compileTerminator(CodegenContext &ctx, const mir::Terminator &term) {
  std::visit(
      [&](auto &&arg) {
        using T = std::decay_t<decltype(arg)>;
        if constexpr (std::is_same_v<T, mir::ReturnTerminator>) {
          if (ctx.Builder->GetInsertBlock()->getParent()->hasFnAttribute(llvm::Attribute::PresplitCoroutine)) {
            auto &Context = ctx.Context;
            auto *Module = ctx.Module.get();

            // 1. Store the return value in the Promise slot
            llvm::Value *retVal = evaluateValue(ctx, arg.value);
            llvm::Value *typedPromise =
                ctx.Builder->CreatePointerCast(ctx.PromiseSlot, llvm::PointerType::getUnqual(retVal->getType()));
            ctx.Builder->CreateStore(retVal, typedPromise);

            // 2. Execute a FINAL Suspend
            llvm::Function *saveFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_save);
            llvm::Value *coroState = ctx.Builder->CreateCall(saveFn, {ctx.CurrentCoroHandle});

            llvm::Function *suspendFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_suspend);
            llvm::Value *isFinal = llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 1);  // FINAL = true!
            llvm::Value *suspendResult = ctx.Builder->CreateCall(suspendFn, {coroState, isFinal});

            // 3. Branch to the unified Suspend/Cleanup blocks
            llvm::SwitchInst *sw = ctx.Builder->CreateSwitch(suspendResult, ctx.CoroSuspendBlock, 2);
            sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0), ctx.CoroCleanupBlock);
            sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1), ctx.CoroCleanupBlock);

          } else {
            if (ctx.Builder->GetInsertBlock()->getParent()->getReturnType()->isVoidTy()) {
              ctx.Builder->CreateRetVoid();
            } else {
              llvm::Value *retVal = evaluateValue(ctx, arg.value);
              ctx.Builder->CreateRet(retVal);
            }
          }
        } else if constexpr (std::is_same_v<T, mir::JumpTerminator>) {
          int target = std::stoi(arg.target);
          ctx.Builder->CreateBr(ctx.Blocks[target]);
        } else if constexpr (std::is_same_v<T, mir::BranchTerminator>) {
          llvm::Value *condVal = evaluateValue(ctx, arg.condition);
          int trueTarget = std::stoi(arg.true_target);
          int falseTarget = std::stoi(arg.false_target);
          ctx.Builder->CreateCondBr(condVal, ctx.Blocks[trueTarget], ctx.Blocks[falseTarget]);
        } else if constexpr (std::is_same_v<T, mir::UnreachableTerminator>) {
          ctx.Builder->CreateUnreachable();
        } else if constexpr (std::is_same_v<T, mir::CoroSuspendTerminator>) {
          auto *Module = ctx.Module.get();
          auto &Context = ctx.Context;

          int resume_target = std::stoi(arg.resume_block);
          int cleanup_target = std::stoi(arg.cleanup_block);
          int suspend_target = std::stoi(arg.suspend_block);

          // 1. llvm.coro.save - Captures the current state of the coroutine
          llvm::Function *saveFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_save);
          llvm::Value *coroState = ctx.Builder->CreateCall(saveFn, {ctx.CurrentCoroHandle}, "coro.state");

          // 2. llvm.coro.suspend - Suspends execution
          llvm::Function *suspendFn = llvm::Intrinsic::getDeclaration(Module, llvm::Intrinsic::coro_suspend);
          llvm::Value *isFinal = llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0);  // Not the final suspend
          llvm::Value *suspendResult = ctx.Builder->CreateCall(suspendFn, {coroState, isFinal}, "suspend.result");

          // 3. Extract the basic blocks we generated in the MIR
          llvm::BasicBlock *resumeBB = ctx.Blocks[resume_target];
          llvm::BasicBlock *cleanupBB = ctx.Blocks[cleanup_target];
          llvm::BasicBlock *suspendBB = ctx.Blocks[suspend_target];

          // 4. Switch on the intrinsic's return value
          llvm::SwitchInst *sw = ctx.Builder->CreateSwitch(suspendResult, suspendBB, 2);
          sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0), resumeBB);
          sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1), cleanupBB);
        }
      },
      term.inner);
}

}  // namespace maml