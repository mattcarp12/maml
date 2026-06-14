#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"

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
          llvm::Value *symVal = ctx.resolveSymbol(arg.name);
          if (!symVal) {
            ctx.Error.fatal("Variable '" + arg.name + "' is not defined in the current scope.");
            return nullptr;
          }
          if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(symVal)) {
            if (alloca->getAllocatedType()->isArrayTy()) {
              return alloca;
            }
            return ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca, arg.name + "_load");
          }
          return symVal;

        } else if constexpr (std::is_same_v<T, mir::IntConstant>) {
          return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), arg.value);

        } else if constexpr (std::is_same_v<T, mir::BoolConstant>) {
          return llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), arg.value ? 1 : 0);

        } else if constexpr (std::is_same_v<T, mir::StringConstant>) {
          auto &Builder = ctx.Builder;
          llvm::Type *strTy = llvm::StructType::get(
              ctx.Context, {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});

          llvm::Constant *strConst = llvm::ConstantDataArray::getString(ctx.Context, arg.value, true);
          llvm::GlobalVariable *globalStr = new llvm::GlobalVariable(
              *ctx.Module, strConst->getType(), true, llvm::GlobalValue::PrivateLinkage, strConst, "str_lit");
          llvm::Function *allocFn = ctx.Module->getFunction(rt::ALLOC);
          const size_t dataSize = arg.value.length() + 1;

          llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), dataSize);
          llvm::Value *heapPtr = Builder->CreateCall(allocFn, {sizeVal}, "str_heap_alloc");

          Builder->CreateMemCpy(heapPtr, llvm::MaybeAlign(1), globalStr, llvm::MaybeAlign(1), dataSize);
          llvm::AllocaInst *headerAlloca = Builder->CreateAlloca(strTy, nullptr, "str_header");

          llvm::Value *dataGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(0)});
          Builder->CreateStore(heapPtr, dataGep);

          llvm::Value *lenGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(1)});
          Builder->CreateStore(Builder->getInt32(arg.value.length()), lenGep);

          return ctx.Builder->CreateLoad(strTy, headerAlloca, "str_literal_val");
        }

        return nullptr;
      },
      val.inner);
}
}  // namespace maml