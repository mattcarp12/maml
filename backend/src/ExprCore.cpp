#include "ExprGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileIdentifier(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  std::string_view varName = expr["value"].get<std::string_view>();
  llvm::Value *val = ctx.resolveSymbol(varName);

  if (!val) {
    if (expr.contains("maml_type") && expr["maml_type"]["kind"] == "SumType") {
      for (const auto &v : expr["maml_type"]["variants"]) {
        if (v["name"].get<std::string_view>() == varName) {
          llvm::Type *sumTy = llvmTypeFor(ctx, expr["maml_type"]);
          llvm::AllocaInst *alloca =
              Builder->CreateAlloca(sumTy, nullptr, std::string("unit_variant_") + std::string(varName));
          llvm::Value *discrimGep = Builder->CreateGEP(sumTy, alloca, {Builder->getInt32(0), Builder->getInt32(0)});
          Builder->CreateStore(Builder->getInt32(v["discriminant"].get<int>()), discrimGep);
          return alloca;
        }
      }
    }
    ctx.Error.fatal("Variable '" + std::string(varName) + "' is not defined in the current scope.", expr);
    return nullptr;
  }

  llvm::Type *expectedTy = llvmTypeFor(ctx, expr["maml_type"]);
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(val)) {
    llvm::Type *allocTy = alloca->getAllocatedType();
    if (allocTy->isArrayTy()) return alloca;
    return Builder->CreateLoad(allocTy, alloca, std::string(varName) + "_load");
  }

  if (val->getType()->isPointerTy() && !expectedTy->isArrayTy()) {
    return Builder->CreateLoad(expectedTy, val, std::string(varName) + "_load");
  }
  return val;
}

llvm::Value *compileHeapAllocExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Type *valTy = llvmTypeFor(ctx, expr["maml_type"]);

  llvm::DataLayout DL(ctx.Module.get());
  uint64_t allocSize = DL.getTypeAllocSize(valTy);

  llvm::FunctionCallee mamlAlloc = ctx.Module->getOrInsertFunction(
      "maml_alloc",
      llvm::FunctionType::get(llvm::PointerType::getUnqual(ctx.Context), {llvm::Type::getInt64Ty(ctx.Context)}, false));

  llvm::Value *heapPtr = Builder->CreateCall(
      mamlAlloc, {llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), allocSize)}, "heap_alloc");

  llvm::Value *innerVal = evaluateExpression(ctx, expr["value"]);
  if (!innerVal) return nullptr;

  // If evaluating the inner value returned a stack pointer (like ArrayLiteral), copy the data.
  if (innerVal->getType()->isPointerTy()) {
    llvm::Value *loaded = Builder->CreateLoad(valTy, innerVal);
    Builder->CreateStore(loaded, heapPtr);
  } else {
    Builder->CreateStore(innerVal, heapPtr);
  }
  return heapPtr;
}

llvm::Value *compileStackAllocExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  // Stack allocations just evaluate their inner literal (which generates an AllocaInst natively)
  return evaluateExpression(ctx, expr["value"]);
}

llvm::Value *evaluateExpression(CodegenContext &ctx, const nlohmann::json &expr) {
  if (expr.is_null()) return nullptr;

  auto &Context = ctx.Context;

  std::string_view nodeType = "unknown";
  if (expr.contains("node_type")) {
    nodeType = expr["node_type"].get<std::string_view>();
  }

  if (nodeType == "IntLiteral")
    return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), expr["value"].get<int>());
  if (nodeType == "BoolLiteral")
    return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), expr["value"].get<bool>() ? 1 : 0);
  if (nodeType == "Identifier") return compileIdentifier(ctx, expr);
  if (nodeType == "PrefixExpr") return compilePrefixExpr(ctx, expr);
  if (nodeType == "InfixExpr") return compileInfixExpr(ctx, expr);
  if (nodeType == "IfExpr") return compileIfExpr(ctx, expr);
  if (nodeType == "BlockStmt") return compileBlockExpr(ctx, expr);
  if (nodeType == "FieldAccess") return compileFieldAccess(ctx, expr);
  if (nodeType == "StringLiteral") return compileStringLiteral(ctx, expr);
  if (nodeType == "VariantLiteral") return compileVariantLiteral(ctx, expr);
  if (nodeType == "SliceExpr") return compileSliceExpr(ctx, expr);
  if (nodeType == "AwaitExpr") return compileAwaitExpression(ctx, expr);
  if (nodeType == "CallExpr") return compileCallExpr(ctx, expr);
  if (nodeType == "IndexExpr") return compileIndexExpr(ctx, expr);
  if (nodeType == "HeapAllocExpr") return compileHeapAllocExpr(ctx, expr);
  if (nodeType == "StackAllocExpr") return compileStackAllocExpr(ctx, expr);
  if (nodeType == "AsyncPrologueExpr") return compileAsyncPrologueExpr(ctx, expr);
  if (nodeType == "ArrayLiteral") return compileArrayLiteral(ctx, expr);
  if (nodeType == "StructLiteral") return compileStructLiteral(ctx, expr);

  ctx.Error.fatal("Unsupported expression node type encountered: " + std::string(nodeType), expr);
  return nullptr;
}

}  // namespace maml