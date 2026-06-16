#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

#include "ExprGenerator.hpp"

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
          return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), arg.value);
        } else if constexpr (std::is_same_v<T, mir::BoolConstant>) {
          return llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), arg.value ? 1 : 0);
        } else if constexpr (std::is_same_v<T, mir::StringConstant>) {
          auto &Builder = ctx.Builder;
          llvm::Type *strTy = llvm::StructType::get(
              ctx.Context, {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});

          const size_t dataSize = arg.value.length() + 1;
          llvm::Constant *strConst = llvm::ConstantDataArray::getString(ctx.Context, arg.value, true);

          // 1. Define a dummy ARC header structure large enough to satisfy Zig's alignment and offsets
          llvm::Type *i64Ty = llvm::Type::getInt64Ty(ctx.Context);

          // Layout: { i64 ref_count, i64 capacity, [N x i8] data }
          llvm::StructType *arcStrTy = llvm::StructType::get(ctx.Context, {i64Ty, i64Ty, strConst->getType()});

          // 2. Set an exceptionally high reference count (e.g., 1 billion) so it never drops to 0
          llvm::Constant *hugeRef = llvm::ConstantInt::get(i64Ty, 0x3FFFFFFF);
          llvm::Constant *arcStructConst = llvm::ConstantStruct::get(arcStrTy, {hugeRef, hugeRef, strConst});

          // 3. Allocate as a global variable in the read-write .data section
          // (isConstant=false is MANDATORY so atomicRmw in maml_retain doesn't segfault on read-only memory)
          llvm::GlobalVariable *globalVar = new llvm::GlobalVariable(
              *ctx.Module, arcStrTy, false, llvm::GlobalValue::PrivateLinkage, arcStructConst, "str_lit_arc");

          // 4. Extract the pointer to the actual string characters (Index 2 skips the dummy headers)
          llvm::Value *dataPtr = Builder->CreateInBoundsGEP(
              arcStrTy, globalVar, {Builder->getInt32(0), Builder->getInt32(2)}, "str_data_ptr");

          llvm::AllocaInst *headerAlloca = Builder->CreateAlloca(strTy, nullptr, "str_header");

          llvm::Value *dataGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(0)});
          Builder->CreateStore(dataPtr, dataGep);

          llvm::Value *lenGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(1)});
          Builder->CreateStore(Builder->getInt32(arg.value.length()), lenGep);

          return ctx.Builder->CreateLoad(strTy, headerAlloca, "str_literal_val");
        }

        return nullptr;
      },
      val.inner);
}
}  // namespace maml