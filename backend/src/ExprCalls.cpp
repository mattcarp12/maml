#include "ExprGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileCallExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;

  // 1. Evaluate arguments FIRST so we have their exact LLVM Types
  std::vector<llvm::Value *> args;
  std::vector<llvm::Type *> argTys;
  for (const auto &arg : expr["arguments"]) {
    llvm::Value *argVal = evaluateExpression(ctx, arg["argument"]);
    if (!argVal) return nullptr;
    args.push_back(argVal);
    argTys.push_back(argVal->getType());
  }

  // 2. Resolve the Callee
  llvm::Value *callee = nullptr;
  std::string_view functionNodeType = "unknown";
  if (expr["function"].contains("node_type")) {
    functionNodeType = expr["function"]["node_type"].get<std::string_view>();
  }

  if (functionNodeType == "Identifier") {
    std::string funcName = expr["function"]["value"].get<std::string>();
    callee = ctx.Module->getFunction(funcName);
    if (!callee) {
      callee = ctx.resolveSymbol(funcName);
    }

    // ---> THE FIX: Lazy-declare external/runtime functions on the fly! <---
    if (!callee) {
      llvm::Type *retTy = llvmTypeFor(ctx, expr["maml_type"]);
      llvm::FunctionType *externalFT = llvm::FunctionType::get(retTy, argTys, false);
      callee = ctx.Module->getOrInsertFunction(funcName, externalFT).getCallee();
    }
  } else {
    callee = evaluateExpression(ctx, expr["function"]);
  }

  // Failsafe
  if (!callee) {
    ctx.Error.fatal("Could not resolve function for call", expr);
    return nullptr;
  }

  // 3. Load if it's an alloca (e.g., executing a function pointer)
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(callee)) {
    callee = Builder->CreateLoad(alloca->getAllocatedType(), alloca, "fn_ptr_load");
  }

  // 4. Construct the Final Function Type for the Call Instruction
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