#include "ProgramGenerator.hpp"

#include <llvm/IR/DerivedTypes.h>
#include <llvm/IR/Intrinsics.h>
#include <llvm/IR/Verifier.h>

#include "RuntimeConstants.h"
#include "StmtGenerator.hpp"
#include "TypeLowering.hpp"
#include "abi_generated.hpp"

namespace maml {

void defineCoroHelperStubs(CodegenContext &ctx) {
  auto &Module = ctx.Module;
  auto &Context = ctx.Context;
  llvm::Type *voidTy = llvm::Type::getVoidTy(Context);
  llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
  llvm::Type *i1Ty = llvm::Type::getInt1Ty(Context);

  // Define the LLVM structure type matching your Future: { ptr, i1 }
  llvm::StructType *futureStructTy = llvm::StructType::get(Context, {ptrTy, i1Ty});

  auto defineIfMissing = [&](const char *name, llvm::FunctionType *FT, auto body) {
    llvm::Function *F = Module->getFunction(name);
    if (!F) {
      F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, name, *Module);
    } else if (!F->isDeclaration()) {
      return;
    }
    llvm::BasicBlock *BB = llvm::BasicBlock::Create(Context, "entry", F);
    ctx.Builder->SetInsertPoint(BB);
    body(F);
  };

  defineIfMissing(rt::CORO_RESUME_HELPER, llvm::FunctionType::get(voidTy, {ptrTy}, false), [&](llvm::Function *F) {
    llvm::Value *hdl = F->getArg(0);
    llvm::Function *resumeFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_resume);
    ctx.Builder->CreateCall(resumeFn, {hdl});
    ctx.Builder->CreateRetVoid();
  });
  defineIfMissing(rt::CORO_DONE_HELPER, llvm::FunctionType::get(i1Ty, {ptrTy}, false), [&](llvm::Function *F) {
    llvm::Value *hdl = F->getArg(0);
    llvm::Function *doneFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_done);
    llvm::Value *isDone = ctx.Builder->CreateCall(doneFn, {hdl});
    ctx.Builder->CreateRet(isDone);
  });
  defineIfMissing(rt::CORO_DESTROY_HELPER, llvm::FunctionType::get(voidTy, {ptrTy}, false), [&](llvm::Function *F) {
    llvm::Value *hdl = F->getArg(0);
    llvm::Function *destroyFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_destroy);
    ctx.Builder->CreateCall(destroyFn, {hdl});
    ctx.Builder->CreateRetVoid();
  });
}

static void allocateVariables(CodegenContext &ctx, llvm::Function *F, const mir::Function &fn) {
  // 1. Allocate Arguments
  unsigned idx = 0;
  for (auto &arg : F->args()) {
    std::string paramName = fn.params[idx].name;
    arg.setName(paramName);
    llvm::AllocaInst *alloca = ctx.Builder->CreateAlloca(arg.getType(), nullptr, paramName);
    ctx.Builder->CreateStore(&arg, alloca);
    ctx.SymbolEnv.back()[paramName] = alloca;
    ctx.SymbolTypes[paramName] = arg.getType();
    idx++;
  }

  // 2. Allocate Locals
  for (const auto &[name, type] : fn.locals) {
    llvm::Type *ty = llvmTypeFor(ctx, type);
    if (ty->isVoidTy()) continue;

    ctx.SymbolTypes[name] = ty;
    llvm::AllocaInst *alloca = ctx.Builder->CreateAlloca(ty, nullptr, name);
    ctx.Builder->CreateStore(llvm::Constant::getNullValue(ty), alloca);
    ctx.SymbolEnv.back()[name] = alloca;
  }
}

static void finalizeCoroutineBlocks(CodegenContext &ctx, llvm::Function *F) {
  // --- Suspend Block (Return to Caller) ---
  ctx.Builder->SetInsertPoint(ctx.CoroSuspendBlock);
  llvm::Function *endFn = llvm::Intrinsic::getOrInsertDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_end);
  llvm::Value *isUnwind = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
  llvm::Value *noneToken = llvm::ConstantTokenNone::get(ctx.Context);
  ctx.Builder->CreateCall(endFn, {ctx.CurrentCoroHandle, isUnwind, noneToken});

  llvm::Type *retTy = F->getReturnType();
  if (retTy->isStructTy()) {
    llvm::Value *futureVal = llvm::UndefValue::get(retTy);
    futureVal = ctx.Builder->CreateInsertValue(futureVal, ctx.CurrentCoroHandle, 0);
    llvm::Value *isDone = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
    futureVal = ctx.Builder->CreateInsertValue(futureVal, isDone, 1);
    ctx.Builder->CreateRet(futureVal);
  } else {
    ctx.Builder->CreateRet(ctx.CurrentCoroHandle);
  }

  // --- Cleanup Block (Destroy Coroutine) ---
  ctx.Builder->SetInsertPoint(ctx.CoroCleanupBlock);
  llvm::Function *freeFn = llvm::Intrinsic::getOrInsertDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_free);
  llvm::Value *memToFree = ctx.Builder->CreateCall(freeFn, {ctx.CoroId, ctx.CurrentCoroHandle});
  llvm::Function *mamlFreeFn = ctx.Module->getFunction("maml_free");
  ctx.Builder->CreateCall(mamlFreeFn, {memToFree});
  ctx.Builder->CreateBr(ctx.CoroSuspendBlock);
}

void declareFunction(CodegenContext &ctx, const mir::Function &fn) {
  if (ctx.Module->getFunction(fn.name)) {
    // ctx.Error.fatal("Function already declared: " + fn.name);
    return;
  }
  llvm::Type *retType = llvm::Type::getVoidTy(ctx.Context);
  if (fn.return_type) retType = llvmTypeFor(ctx, fn.return_type);
  if (fn.name == "main") retType = llvm::Type::getInt32Ty(ctx.Context);

  std::vector<llvm::Type *> paramTypes;
  for (const auto &p : fn.params) paramTypes.push_back(llvmTypeFor(ctx, p.type));

  llvm::FunctionType *FT = llvm::FunctionType::get(retType, paramTypes, false);
  llvm::Function *F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, fn.name, *ctx.Module);
  if (fn.is_async) F->addFnAttr(llvm::Attribute::PresplitCoroutine);
}

void compileFunction(CodegenContext &ctx, const mir::Function &fn) {
  ctx.CurrentFunctionName = fn.name;

  // 1. Retrieve pre-declared signature
  llvm::Function *F = ctx.Module->getFunction(fn.name);
  if (!F) {
    ctx.Error.fatal("Function not declared in pass 1: " + fn.name);
  }

  // Extern functions only need declarations, no bodies
  if (fn.is_extern) return;

  ctx.pushScope();

  // 2. Setup Entry & Async Blocks
  llvm::BasicBlock *allocBB = llvm::BasicBlock::Create(ctx.Context, "entry_allocs", F);
  if (fn.is_async) {
    ctx.CoroSuspendBlock = llvm::BasicBlock::Create(ctx.Context, "coro.suspend.ret", F);
    ctx.CoroCleanupBlock = llvm::BasicBlock::Create(ctx.Context, "coro.cleanup", F);
    if (!fn.entry_block.empty()) ctx.CoroEntryBlockId = std::stoi(fn.entry_block);
  }

  ctx.Builder->SetInsertPoint(allocBB);

  if (fn.name == "main") {
    if (llvm::Function *initFn = ctx.Module->getFunction("maml_coro_runtime_init")) {
      ctx.Builder->CreateCall(initFn, {});
    }
  }

  // 3. Allocate Variables
  allocateVariables(ctx, F, fn);

  // 4. Map MIR Blocks
  if (!fn.blocks.empty()) {
    // Pre-create all basic blocks
    for (const auto &block : fn.blocks) {
      if (block.id.empty()) ctx.Error.fatal("Encountered block with missing ID!");
      ctx.Blocks[std::stoi(block.id)] = llvm::BasicBlock::Create(ctx.Context, "bb" + block.id, F);
    }

    // Isolate and compile the async prologue first
    if (fn.is_async) {
      bool foundPrologue = false;
      for (const auto &block : fn.blocks) {
        for (const auto &inst : block.instructions) {
          if (std::holds_alternative<mir::CoroPrologueInst>(inst.inner)) {
            compileInstruction(ctx, inst);
            foundPrologue = true;
            break;
          }
        }
        if (foundPrologue) break;
      }
    }

    if (!fn.entry_block.empty() && !fn.is_async) {
      ctx.Builder->CreateBr(ctx.Blocks[std::stoi(fn.entry_block)]);
    }

    // Compile Instructions & Terminators
    for (const auto &block : fn.blocks) {
      ctx.Builder->SetInsertPoint(ctx.Blocks[std::stoi(block.id)]);

      ctx.CurrentInstructionName = "Instructions (Block " + block.id + ")";
      for (const auto &inst : block.instructions) {
        if (!std::holds_alternative<mir::CoroPrologueInst>(inst.inner)) {
          compileInstruction(ctx, inst);
        }
      }

      ctx.CurrentInstructionName = "Terminator (Block " + block.id + ")";
      compileTerminator(ctx, block.terminator);
    }
  } else {
    ctx.Builder->CreateRetVoid();
  }

  // 5. Finalize Async State
  if (fn.is_async) {
    finalizeCoroutineBlocks(ctx, F);
  }

  ctx.popScope();
  ctx.Blocks.clear();

  if (llvm::verifyFunction(*F, &llvm::errs())) {
    ctx.Error.fatal("LLVM Verification failed for function: " + fn.name);
  }
}

void compileProgram(CodegenContext &ctx, const mir::Program &prog) {
  rt::declareRuntimeFunctions(ctx);
  defineCoroHelperStubs(ctx);

  // Pass 1: Declare all function signatures
  for (const auto &fn : prog.functions) {
    declareFunction(ctx, fn);
  }

  // Pass 2: Compile function bodies
  for (const auto &fn : prog.functions) {
    compileFunction(ctx, fn);
  }

  // --- Final Module Verification ---
  if (llvm::verifyModule(*ctx.Module, &llvm::errs())) {
    ctx.Error.fatal("LLVM Module Verification failed! Check the console output above for details.");
  }
}

}  // namespace maml