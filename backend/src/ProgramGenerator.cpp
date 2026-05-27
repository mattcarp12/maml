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

  // 1. Resolve Return Type
  llvm::Type *retType = llvm::Type::getVoidTy(Context);
  if (fn.contains("return_type") && !fn["return_type"].is_null()) {
    retType = llvmTypeFor(ctx, fn["return_type"]);
  }
  if (name == "main") {
    retType = llvm::Type::getInt32Ty(Context);
  }

  // 2. Resolve Parameter Types
  std::vector<llvm::Type *> paramTypes;
  if (fn.contains("params")) {
    for (const auto &p : fn["params"]) {
      paramTypes.push_back(llvmTypeFor(ctx, p["type"]));
    }
  }

  // 3. Create the LLVM Function Signature
  llvm::StringRef llName(name.data(), name.size());
  llvm::FunctionType *FT = llvm::FunctionType::get(retType, paramTypes, false);
  llvm::Function *F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, llName, *Module);

  if (fn.contains("is_async") && fn["is_async"] == true) {
    F->addFnAttr(llvm::Attribute::PresplitCoroutine);
  }

  // 4. Reset Context State for the new function
  ctx.SymbolTable.clear();
  ctx.Blocks.clear();
  ctx.pushScope();

  // ===========================================================================
  // Basic Block Materialization
  // ===========================================================================

  // Pre-allocate all basic blocks in LLVM before compiling any instructions.
  // This mathematically guarantees forward-jumping terminators never fail.
  if (fn.contains("blocks")) {
    for (auto it = fn["blocks"].begin(); it != fn["blocks"].end(); ++it) {
      int blockId = std::stoi(it.key());
      llvm::BasicBlock *bb = llvm::BasicBlock::Create(Context, "bb_" + std::to_string(blockId), F);
      ctx.Blocks[blockId] = bb;
    }
  }

  // ===========================================================================
  // Entry Point Setup & Parameter Allocation
  // ===========================================================================

  if (fn.contains("entry_block")) {
    int entryId = fn["entry_block"].get<int>();
    Builder->SetInsertPoint(ctx.Blocks[entryId]);

    // Materialize the universal "_ret" register if the function returns a value
    if (!retType->isVoidTy() && !fn.value("is_async", false)) {
      llvm::AllocaInst *retAlloc = Builder->CreateAlloca(retType, nullptr, "_ret");
      ctx.SymbolEnv.back()["_ret"] = retAlloc;
    }

    // Materialize arguments as stack-allocated variables in the entry block
    unsigned Idx = 0;
    for (auto &Arg : F->args()) {
      std::string_view pName = fn["params"][Idx]["name"].get<std::string_view>();
      llvm::Type *pType = paramTypes[Idx];
      llvm::AllocaInst *alloca = Builder->CreateAlloca(pType, nullptr, std::string(pName));
      Builder->CreateStore(&Arg, alloca);
      ctx.SymbolEnv.back()[std::string(pName)] = alloca;
      Idx++;
    }

    // ===========================================================================
    // PHASE 9, STEP 3, 4, 5: Linear Instruction Translation
    // ===========================================================================

    if (fn.contains("blocks")) {
      // 1. Gather and sort block IDs numerically to prevent lexicographical sorting corruption
      std::vector<int> sortedBlockIds;
      for (auto it = fn["blocks"].begin(); it != fn["blocks"].end(); ++it) {
        sortedBlockIds.push_back(std::stoi(it.key()));
      }
      std::sort(sortedBlockIds.begin(), sortedBlockIds.end());

      // 2. Translate statements strictly following the sorted flow control sequence
      for (int blockId : sortedBlockIds) {
        std::string blockKey = std::to_string(blockId);
        const auto &blockData = fn["blocks"][blockKey];

        Builder->SetInsertPoint(ctx.Blocks[blockId]);

        if (blockData.contains("statements")) {
          for (const auto &stmt : blockData["statements"]) {
            compileStatement(ctx, stmt);
          }
        }

        // Emit the strict block exit routing!
        compileTerminator(ctx, blockData["terminator"]);
      }
    }
  }

  // Scope cleanup
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