#include "StmtGenerator.h"

#include "ExprGenerator.h"

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
void handle(CodegenContext &ctx, const mir::MutBorrowInst &inst);
void handle(CodegenContext &ctx, const mir::CoroPrologueInst &inst);
void handle(CodegenContext &ctx, const mir::KeepAliveInst &inst);

void compileInstruction(CodegenContext &ctx, const mir::Instruction &inst) {
  std::visit([&](auto &&arg) { handle(ctx, arg); }, inst.inner);
}

void compileTerminator(CodegenContext &ctx, const mir::Terminator &term) {
  std::visit(
      [&](auto &&arg) {
        using T = std::decay_t<decltype(arg)>;
        if constexpr (std::is_same_v<T, mir::ReturnTerminator>) {
          if (ctx.Builder->GetInsertBlock()->getParent()->getReturnType()->isVoidTy()) {
            ctx.Builder->CreateRetVoid();
          } else {
            llvm::Value *retVal = evaluateValue(ctx, arg.value);
            ctx.Builder->CreateRet(retVal);
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
          // Unimplemented
        }
      },
      term.inner);
}

}  // namespace maml