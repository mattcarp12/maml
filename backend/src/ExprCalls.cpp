#include "ExprGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileCallExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  llvm::Value *callee = nullptr;

  std::string_view functionNodeType = "unknown";
  if (expr["function"].contains("node_type")) {
    functionNodeType = expr["function"]["node_type"].get<std::string_view>();
  }

  if (functionNodeType == "Identifier") {
    std::string funcName = expr["function"]["value"].get<std::string>();
    callee = ctx.Module->getFunction(funcName);
    if (!callee) {
      // Try resolving from local scope (closures / function pointers)
      callee = ctx.resolveSymbol(funcName);
    }
  } else {
    callee = evaluateExpression(ctx, expr["function"]);
  }

  if (!callee) {
    ctx.Error.fatal("Could not resolve function for call", expr);
    return nullptr;
  }

  // Load if it's an alloca
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(callee)) {
    callee = Builder->CreateLoad(alloca->getAllocatedType(), alloca, "fn_ptr_load");
  }

  std::vector<llvm::Value *> args;
  std::vector<llvm::Type *> argTys;
  for (const auto &arg : expr["arguments"]) {
    llvm::Value *argVal = evaluateExpression(ctx, arg["argument"]);
    llvm::Type *expectedTy = llvmTypeFor(ctx, arg["argument"]["maml_type"]);

    if (argVal->getType()->isPointerTy() && !expectedTy->isPointerTy() && !expectedTy->isArrayTy()) {
      argVal = Builder->CreateLoad(expectedTy, argVal, "arg_load");
    }
    args.push_back(argVal);
    argTys.push_back(argVal->getType());
  }

  llvm::FunctionType *FT = nullptr;
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    FT = F->getFunctionType();
  } else {
    llvm::Type *retTy = llvmTypeFor(ctx, expr["maml_type"]);
    FT = llvm::FunctionType::get(retTy, argTys, false);
  }

  return Builder->CreateCall(FT, callee, args, "calltmp");
}

}  // namespace maml