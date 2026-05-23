#include "ProgramGenerator.h"

#include <llvm/IR/Intrinsics.h>

#include "RuntimeConstants.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

void compileFunction(CodegenContext &ctx, const nlohmann::json &fn) {
  auto &Builder = ctx.Builder;
  auto &Context = ctx.Context;
  auto &Module = ctx.Module;

  std::string_view name = fn["name"].get<std::string_view>();

  llvm::Type *retType = llvm::Type::getVoidTy(Context);
  if (fn.contains("maml_type") && !fn["maml_type"].is_null()) {
    retType = llvmTypeFor(ctx, fn["maml_type"]);
  } else if (fn.contains("return_type") && !fn["return_type"].is_null()) {
    retType = llvmTypeFor(ctx, fn["return_type"]);
  }

  if (name == "main") {
    retType = llvm::Type::getInt32Ty(Context);
  }

  std::vector<llvm::Type *> paramTypes;
  if (fn.contains("params")) {
    for (const auto &p : fn["params"]) {
      paramTypes.push_back(llvmTypeFor(ctx, p["type"]));
    }
  }

  llvm::StringRef llName(name.data(), name.size());
  llvm::FunctionType *FT = llvm::FunctionType::get(retType, paramTypes, false);
  llvm::Function *F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, llName, *Module);

  if (fn.contains("is_async") && fn["is_async"] == true) {
    F->addFnAttr(llvm::Attribute::PresplitCoroutine);
  }

  llvm::BasicBlock *BB = llvm::BasicBlock::Create(Context, "entry", F);
  Builder->SetInsertPoint(BB);

  ctx.SymbolTable.clear();
  ctx.pushScope();

  unsigned Idx = 0;
  for (auto &Arg : F->args()) {
    std::string_view pName = fn["params"][Idx]["name"].get<std::string_view>();
    llvm::Type *pType = paramTypes[Idx];
    llvm::AllocaInst *alloca = Builder->CreateAlloca(pType, nullptr, std::string(pName));
    Builder->CreateStore(&Arg, alloca);
    ctx.SymbolEnv.back()[std::string(pName)] = alloca;
    Idx++;
  }

  if (fn.contains("body")) {
    compileStatement(ctx, fn["body"]);
  }

  if (!Builder->GetInsertBlock()->getTerminator()) {
    if (llvm::Value *coroHdl = ctx.resolveSymbol(rt::CORO_HDL)) {
      Builder->CreateRet(coroHdl);
    } else if (name == "main") {
      Builder->CreateRet(llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0));
    } else if (retType->isVoidTy()) {
      Builder->CreateRetVoid();
    } else {
      Builder->CreateRet(llvm::Constant::getNullValue(retType));
    }
  }

  if (!ctx.SymbolEnv.empty()) {
    ctx.popScope();
  }
}

static void generateCoroutineHelpers(CodegenContext &ctx) {
  auto &Builder = ctx.Builder;
  auto &Context = ctx.Context;
  auto &Module = ctx.Module;

  llvm::Type *voidTy = llvm::Type::getVoidTy(Context);
  llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
  llvm::Type *boolTy = llvm::Type::getInt1Ty(Context);

  llvm::Function *coroResumeIntrin = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_resume);
  llvm::Function *coroDestroyIntrin = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_destroy);
  llvm::Function *coroDoneIntrin = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_done);

  llvm::Function *resumeHelper = llvm::Function::Create(llvm::FunctionType::get(voidTy, {ptrTy}, false),
                                                        llvm::Function::ExternalLinkage, rt::CORO_RESUME, *Module);
  llvm::BasicBlock *resumeBB = llvm::BasicBlock::Create(Context, "entry", resumeHelper);
  Builder->SetInsertPoint(resumeBB);
  Builder->CreateCall(coroResumeIntrin, {resumeHelper->getArg(0)});
  Builder->CreateRetVoid();

  llvm::Function *destroyHelper = llvm::Function::Create(llvm::FunctionType::get(voidTy, {ptrTy}, false),
                                                         llvm::Function::ExternalLinkage, rt::CORO_DESTROY, *Module);
  llvm::BasicBlock *destroyBB = llvm::BasicBlock::Create(Context, "entry", destroyHelper);
  Builder->SetInsertPoint(destroyBB);
  Builder->CreateCall(coroDestroyIntrin, {destroyHelper->getArg(0)});
  Builder->CreateRetVoid();

  llvm::Function *doneHelper = llvm::Function::Create(llvm::FunctionType::get(boolTy, {ptrTy}, false),
                                                      llvm::Function::ExternalLinkage, rt::CORO_DONE, *Module);
  llvm::BasicBlock *doneBB = llvm::BasicBlock::Create(Context, "entry", doneHelper);
  Builder->SetInsertPoint(doneBB);
  llvm::Value *doneResult = Builder->CreateCall(coroDoneIntrin, {doneHelper->getArg(0)});
  Builder->CreateRet(doneResult);
}

void compileProgram(CodegenContext &ctx, const nlohmann::json &ast) {
  std::string_view nodeType = "unknown";
  if (ast.contains("node_type")) nodeType = ast["node_type"].get<std::string_view>();

  if (nodeType != "Program") return;

  generateCoroutineHelpers(ctx);

  for (const auto &decl : ast["decls"]) {
    std::string_view declNodeType = "unknown";
    if (decl.contains("node_type")) declNodeType = decl["node_type"].get<std::string_view>();

    if (declNodeType == "FnDecl") {
      compileFunction(ctx, decl);
    }
  }
}

}  // namespace maml