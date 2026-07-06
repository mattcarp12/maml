#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"

namespace maml {

llvm::Value *evaluateValue(CodegenContext &ctx, const mir::Value &val) {
  return std::visit(
      [&](auto &&arg) -> llvm::Value * {
        using T = std::decay_t<decltype(arg)>;

        if constexpr (std::is_same_v<T, mir::Register>) {
          if (arg.name == "null") {
            return llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));
          }
          if (llvm::Function *func = ctx.Module->getFunction(arg.name)) {
            return func;
          }

          llvm::Value *rawSym = ctx.resolveSymbol(arg.name);
          if (!rawSym) {
            ctx.Error.fatal("Variable '" + arg.name + "' is not defined in the current scope.");
            return nullptr;
          }

          // Distinguish between memory pointers and raw SSA values
          if (llvm::isa<llvm::AllocaInst>(rawSym)) {
            // 1. Get the actual memory base (handles dereferencing heap pointers automatically)
            llvm::Value *basePtr = ctx.getMemoryBase(arg.name);

            // 2. Fetch the true structural type, immune to LLVM opaque pointers
            llvm::Type *ty = ctx.SymbolTypes[arg.name];

            if (ty && ty->isArrayTy()) {
              // Arrays evaluate to their memory pointer in MAML
              return basePtr;
            }

            // 3. Safely load the full value from the memory address
            return ctx.Builder->CreateLoad(ty, basePtr, arg.name + "_val");
          }

          // If it's not an AllocaInst, it was already evaluated to an SSA value
          return rawSym;

        } else if constexpr (std::is_same_v<T, mir::IntConstant>) {
          llvm::Type *targetTy = llvmTypeFor(ctx, arg.type);
          return llvm::ConstantInt::get(targetTy, arg.value);
        } else if constexpr (std::is_same_v<T, mir::BoolConstant>) {
          return llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), arg.value ? 1 : 0);
        } else if constexpr (std::is_same_v<T, mir::StringConstant>) {
          llvm::Constant *strConst = llvm::ConstantDataArray::getString(ctx.Context, arg.value, true);
          llvm::GlobalVariable *globalVar = new llvm::GlobalVariable(
              *ctx.Module, strConst->getType(), true, llvm::GlobalValue::PrivateLinkage, strConst, "str_lit");
          return ctx.Builder->CreatePointerCast(globalVar, llvm::PointerType::getUnqual(ctx.Context));
        }

        return nullptr;
      },
      val.inner);
}

llvm::Value *evaluateAddress(CodegenContext &ctx, const mir::Value &val) {
  return std::visit(
      [&](auto &&arg) -> llvm::Value * {
        using T = std::decay_t<decltype(arg)>;
        if constexpr (std::is_same_v<T, mir::Register>) {
          llvm::Value *rawSym = ctx.resolveSymbol(arg.name);
          if (!rawSym) {
            ctx.Error.fatal("Variable '" + arg.name + "' is not defined in the current scope.");
            return nullptr;
          }
          if (llvm::isa<llvm::AllocaInst>(rawSym)) {
            llvm::Type *ty = ctx.SymbolTypes[arg.name];
            if (ty && ty->isPointerTy()) {
              return ctx.Builder->CreateLoad(ty, rawSym, arg.name + "_addr");
            }
            return rawSym;
          }
          return rawSym;
        }
        ctx.Error.fatal("Cannot take the address of a non-identifier value.");
        return nullptr;
      },
      val.inner);
}

}  // namespace maml