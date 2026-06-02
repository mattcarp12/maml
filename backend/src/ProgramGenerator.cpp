#include "ProgramGenerator.h"

#include <llvm/IR/Intrinsics.h>

#include "RuntimeConstants.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

void defineCoroHelperStubs(CodegenContext &ctx) {
  auto &Module = ctx.Module;
  auto &Context = ctx.Context;
  llvm::Type *voidTy = llvm::Type::getVoidTy(Context);
  llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
  llvm::Type *i1Ty = llvm::Type::getInt1Ty(Context);

  // Define BEFORE any declarations to prevent LLVM suffixing
  auto defineIfMissing = [&](const char *name, llvm::FunctionType *FT, auto body) {
    if (Module->getFunction(name)) return;  // already exists

    llvm::Function *F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, name, *Module);

    llvm::BasicBlock *BB = llvm::BasicBlock::Create(Context, "entry", F);
    ctx.Builder->SetInsertPoint(BB);
    body(F);
  };

  // maml_coro_resume_helper
  defineIfMissing(rt::CORO_RESUME, llvm::FunctionType::get(voidTy, {ptrTy}, false),
                  [&](llvm::Function *) { ctx.Builder->CreateRetVoid(); });

  // maml_coro_done_helper
  defineIfMissing(rt::CORO_DONE, llvm::FunctionType::get(i1Ty, {ptrTy}, false),
                  [&](llvm::Function *) { ctx.Builder->CreateRet(llvm::ConstantInt::get(i1Ty, 0)); });

  // maml_coro_destroy_helper
  defineIfMissing(rt::CORO_DESTROY, llvm::FunctionType::get(voidTy, {ptrTy}, false),
                  [&](llvm::Function *) { ctx.Builder->CreateRetVoid(); });
}

void declareRuntimeFunctions(CodegenContext &ctx) {
  auto &Module = ctx.Module;
  auto &Context = ctx.Context;
  llvm::Type *voidTy = llvm::Type::getVoidTy(Context);
  llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
  llvm::Type *i64Ty = llvm::Type::getInt64Ty(Context);
  llvm::Type *i32Ty = llvm::Type::getInt32Ty(Context);
  llvm::Type *i8Ty = llvm::Type::getInt8Ty(Context);

  // maml_alloc: ptr (i64)
  Module->getOrInsertFunction(rt::ALLOC, llvm::FunctionType::get(ptrTy, {i64Ty}, false));
  // maml_free: void (ptr)
  Module->getOrInsertFunction(rt::FREE, llvm::FunctionType::get(voidTy, {ptrTy}, false));
  // maml_retain: void (ptr)
  Module->getOrInsertFunction(rt::RETAIN, llvm::FunctionType::get(voidTy, {ptrTy}, false));
  // maml_release: void (ptr)
  Module->getOrInsertFunction(rt::RELEASE, llvm::FunctionType::get(voidTy, {ptrTy}, false));
  // maml_vec_grow: ptr (ptr, i32, ptr, i32)
  Module->getOrInsertFunction(rt::VEC_GROW, llvm::FunctionType::get(ptrTy, {ptrTy, i32Ty, ptrTy, i32Ty}, false));
  Module->getOrInsertFunction(rt::VEC_PUSH, llvm::FunctionType::get(voidTy, {ptrTy, ptrTy}, false));
  // maml_map_create: ptr (i32, i8)
  Module->getOrInsertFunction(rt::MAP_CREATE, llvm::FunctionType::get(ptrTy, {i32Ty, i8Ty}, false));
  // maml_map_put: void (ptr, i64, ptr, ptr, i32)
  Module->getOrInsertFunction(rt::MAP_PUT, llvm::FunctionType::get(voidTy, {ptrTy, i64Ty, ptrTy, ptrTy, i32Ty}, false));
  // maml_map_get: ptr (ptr, i64, ptr, i32)
  Module->getOrInsertFunction(rt::MAP_GET, llvm::FunctionType::get(ptrTy, {ptrTy, i64Ty, ptrTy, i32Ty}, false));
  // maml_str_hash: i64 (ptr, i32)
  Module->getOrInsertFunction(rt::STR_HASH, llvm::FunctionType::get(i64Ty, {ptrTy, i32Ty}, false));
  // puts: i32 (ptr)
  Module->getOrInsertFunction(rt::PUTS, llvm::FunctionType::get(i32Ty, {ptrTy}, false));
}

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

  // Hardcode main to return i32 for C ABI compatibility
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

  // 4. Create a new Variable Scope
  ctx.pushScope();

  // 5. Setup an entry block specifically for stack allocations
  llvm::BasicBlock *allocBB = llvm::BasicBlock::Create(Context, "entry_allocs", F);
  Builder->SetInsertPoint(allocBB);

  // 6. Bind Arguments to Scope
  unsigned idx = 0;
  if (fn.contains("params")) {
    for (auto &arg : F->args()) {
      std::string paramName = fn["params"][idx]["name"].get<std::string>();
      arg.setName(paramName);

      llvm::AllocaInst *alloca = Builder->CreateAlloca(arg.getType(), nullptr, paramName);
      Builder->CreateStore(&arg, alloca);

      ctx.SymbolEnv.back()[paramName] = alloca;
      idx++;
    }
  }

  if (!retType->isVoidTy()) {
    llvm::AllocaInst *retAlloca = Builder->CreateAlloca(retType, nullptr, "_ret");
    // Zero-initialize to prevent undefined behavior on early returns
    Builder->CreateStore(llvm::Constant::getNullValue(retType), retAlloca);
    ctx.SymbolEnv.back()["_ret"] = retAlloca;
  }

  // 7. Setup Basic Blocks (Flattened MIR)
  if (fn.contains("blocks") && !fn["blocks"].is_null()) {
    // 1. Create all basic blocks (Fixed for Array iteration!)
    for (const auto &blockJson : fn["blocks"]) {
      std::string idStr = blockJson["id"].get<std::string>();
      int blockId = std::stoi(idStr);
      llvm::BasicBlock *BB = llvm::BasicBlock::Create(Context, "bb" + idStr, F);
      ctx.Blocks[blockId] = BB;
    }

    // 🌟 2. THE ALLOCA PASS (Hoisting)
    // Point the builder at your dedicated allocation block at the top of the function
    Builder->SetInsertPoint(allocBB);

    // Scan all blocks and compile ONLY 'temp_decl' memory allocations into this entry block.
    // This populates the SymbolEnv globally before any logic runs.
    for (const auto &blockJson : fn["blocks"]) {
      for (const auto &instJson : blockJson["instructions"]) {
        if (instJson["op"] == "temp_decl") {
          compileStatement(ctx, instJson);
        }
      }
    }

    // 3. Terminate the allocation block by branching into the actual MIR control flow
    if (fn.contains("entry_block")) {
      std::string entryStr = fn["entry_block"].get<std::string>();
      int entryId = std::stoi(entryStr);
      Builder->CreateBr(ctx.Blocks[entryId]);
    } else {
      ctx.Error.fatal("Function missing 'entry_block'", fn);
    }

    // 🌟 4. THE LOGIC PASS
    // Now that all variables exist in memory, iterate through again to emit the math,
    // assignments, branches, and function calls into their respective blocks.
    for (const auto &blockJson : fn["blocks"]) {
      int id = std::stoi(blockJson["id"].get<std::string>());
      ctx.Builder->SetInsertPoint(ctx.Blocks[id]);

      for (const auto &instJson : blockJson["instructions"]) {
        // Skip temp_decl because we already hoisted them!
        if (instJson["op"] != "temp_decl") {
          compileStatement(ctx, instJson);
        }
      }
      compileTerminator(ctx, blockJson["terminator"]);
    }
  } else {
    Builder->CreateRetVoid();
  }

  // 8. Cleanup Scope
  ctx.popScope();
  ctx.Blocks.clear();
}

void compileProgram(CodegenContext &ctx, const nlohmann::json &ast) {
  if (ast.is_null()) return;

  defineCoroHelperStubs(ctx);

  declareRuntimeFunctions(ctx);

  // Loop through the explicit "functions" array from the DTO exporter
  if (ast.contains("functions") && ast["functions"].is_array()) {
    for (const auto &fn : ast["functions"]) {
      compileFunction(ctx, fn);
    }
  } else {
    ctx.Error.fatal("Invalid MIR format: missing 'functions' array", ast);
  }
}

}  // namespace maml