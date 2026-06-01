
#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"

namespace maml {

llvm::Value *compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view varName = expr["value"].get<std::string_view>();
  llvm::Value *val = ctx.resolveSymbol(varName);

  if (!val) {
    ctx.Error.fatal("Variable '" + std::string(varName) + "' is not defined in the current scope.", expr);
    return nullptr;
  }

  // In our flattened MIR, all local variables are explicit pointers (Allocas).
  // When evaluating an identifier as an expression, we implicitly load its value.
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(val)) {
    return Builder->CreateLoad(alloca->getAllocatedType(), alloca, std::string(varName) + "_load");
  }

  return val;
}

llvm::Value *compileStringLiteral(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view strVal = expr["value"].get<std::string_view>();

  llvm::Type *strTy = llvm::StructType::get(
      ctx.Context, {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});

  llvm::Constant *strConst = llvm::ConstantDataArray::getString(ctx.Context, strVal, true);
  llvm::GlobalVariable *globalStr = new llvm::GlobalVariable(*ctx.Module, strConst->getType(), true,
                                                             llvm::GlobalValue::PrivateLinkage, strConst, "str_lit");

  llvm::Function *allocFn = ctx.Module->getFunction(rt::ALLOC);
  const size_t dataSize = strVal.length() + 1;

  llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), dataSize);
  llvm::Value *heapPtr = Builder->CreateCall(allocFn, {sizeVal}, "str_heap_alloc");

  // Copy the string data
  Builder->CreateMemCpy(heapPtr, llvm::MaybeAlign(1), globalStr, llvm::MaybeAlign(1), dataSize);

  // Create the fat pointer on stack
  llvm::AllocaInst *headerAlloca = Builder->CreateAlloca(strTy, nullptr, "str_header");

  // Store data pointer
  llvm::Value *dataGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(0)});
  Builder->CreateStore(heapPtr, dataGep);

  // Store length (excluding null)
  llvm::Value *lenGep = Builder->CreateGEP(strTy, headerAlloca, {Builder->getInt32(0), Builder->getInt32(1)});
  Builder->CreateStore(Builder->getInt32(strVal.length()), lenGep);

  // Return the loaded struct value
  return ctx.Builder->CreateLoad(strTy, headerAlloca, "str_literal_val");
}

llvm::Value *evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr) {
  if (expr.is_null()) return nullptr;
  auto &Context = ctx.Context;

  std::string_view op = "unknown";
  if (expr.contains("op")) {
    op = expr["op"].get<std::string_view>();
  }

  // Because the MIR is flat, we ONLY handle atomic operands here.
  if (op == "const_int") return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), expr["value"].get<int64_t>());
  if (op == "const_bool")
    return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), expr["value"].get<bool>() ? 1 : 0);
  if (op == "const_string") return compileStringLiteral(ctx, expr);
  if (op == "ident") return compileIdentifier(ctx, expr);

  // If we reach this, the frontend failed to flatten an expression and leaked it to the backend.
  ctx.Error.fatal("CRITICAL ERROR: Unflattened AST expression node reached backend! Operator: " + std::string(op),
                  expr);
  return nullptr;
}

}  // namespace maml