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
    llvm::Value *futurePtr = F->getArg(0);

    // GEP to the first field of the Future struct: the raw coroutine frame pointer
    llvm::Value *framePtrAddr = ctx.Builder->CreateStructGEP(futureStructTy, futurePtr, 0, "frame_ptr_addr");
    llvm::Value *hdl = ctx.Builder->CreateLoad(ptrTy, framePtrAddr, "coro_hdl");

    llvm::Function *resumeFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_resume);
    ctx.Builder->CreateCall(resumeFn, {hdl});
    ctx.Builder->CreateRetVoid();
  });

  defineIfMissing(rt::CORO_DONE_HELPER, llvm::FunctionType::get(i1Ty, {ptrTy}, false), [&](llvm::Function *F) {
    llvm::Value *futurePtr = F->getArg(0);

    // Extract raw coroutine frame pointer
    llvm::Value *framePtrAddr = ctx.Builder->CreateStructGEP(futureStructTy, futurePtr, 0, "frame_ptr_addr");
    llvm::Value *hdl = ctx.Builder->CreateLoad(ptrTy, framePtrAddr, "coro_hdl");

    llvm::Function *doneFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_done);
    llvm::Value *isDone = ctx.Builder->CreateCall(doneFn, {hdl});
    ctx.Builder->CreateRet(isDone);
  });

  defineIfMissing(rt::CORO_DESTROY_HELPER, llvm::FunctionType::get(voidTy, {ptrTy}, false), [&](llvm::Function *F) {
    llvm::Value *futurePtr = F->getArg(0);

    // Extract raw coroutine frame pointer
    llvm::Value *framePtrAddr = ctx.Builder->CreateStructGEP(futureStructTy, futurePtr, 0, "frame_ptr_addr");
    llvm::Value *hdl = ctx.Builder->CreateLoad(ptrTy, framePtrAddr, "coro_hdl");

    llvm::Function *destroyFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_destroy);
    ctx.Builder->CreateCall(destroyFn, {hdl});
    ctx.Builder->CreateRetVoid();
  });
}

void compileFunction(CodegenContext &ctx, const mir::Function &fn) {
  ctx.CurrentFunctionName = fn.name;
  llvm::Type *retType = llvm::Type::getVoidTy(ctx.Context);
  if (fn.return_type) {
    retType = llvmTypeFor(ctx, fn.return_type);
  }
  if (fn.name == "main") {
    retType = llvm::Type::getInt32Ty(ctx.Context);
  }

  std::vector<llvm::Type *> paramTypes;
  for (const auto &p : fn.params) {
    paramTypes.push_back(llvmTypeFor(ctx, p.type));
  }

  llvm::FunctionType *FT = llvm::FunctionType::get(retType, paramTypes, false);
  llvm::Function *F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, fn.name, *ctx.Module);
  if (fn.is_async) F->addFnAttr(llvm::Attribute::PresplitCoroutine);

  if (fn.is_extern) {
    return;
  }

  ctx.pushScope();

  llvm::BasicBlock *allocBB = llvm::BasicBlock::Create(ctx.Context, "entry_allocs", F);

  if (fn.is_async) {
    ctx.CoroSuspendBlock = llvm::BasicBlock::Create(ctx.Context, "coro.suspend.ret", F);
    ctx.CoroCleanupBlock = llvm::BasicBlock::Create(ctx.Context, "coro.cleanup", F);
    if (!fn.entry_block.empty()) {
      ctx.CoroEntryBlockId = std::stoi(fn.entry_block);
    }
  }

  ctx.Builder->SetInsertPoint(allocBB);

  if (fn.name == "main") {
    llvm::Function *initFn = ctx.Module->getFunction("maml_coro_runtime_init");
    if (initFn) {
      ctx.Builder->CreateCall(initFn, {});
    }
  }

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

  try {
    if (!fn.blocks.empty()) {
      for (const auto &block : fn.blocks) {
        if (block.id.empty()) ctx.Error.fatal("Encountered block with missing ID!");
        int blockId = std::stoi(block.id);
        llvm::BasicBlock *BB = llvm::BasicBlock::Create(ctx.Context, "bb" + block.id, F);
        ctx.Blocks[blockId] = BB;
      }

      for (const auto &[name, type] : fn.locals) {
        llvm::Type *ty = llvmTypeFor(ctx, type);
        if (ty->isVoidTy()) continue;

        ctx.SymbolTypes[name] = ty;
        llvm::AllocaInst *alloca = ctx.Builder->CreateAlloca(ty, nullptr, name);
        ctx.Builder->CreateStore(llvm::Constant::getNullValue(ty), alloca);
        ctx.SymbolEnv.back()[name] = alloca;
      }

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
        int entryId = std::stoi(fn.entry_block);
        ctx.Builder->CreateBr(ctx.Blocks[entryId]);
      }

      for (const auto &block : fn.blocks) {
        int id = std::stoi(block.id);
        ctx.Builder->SetInsertPoint(ctx.Blocks[id]);

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
  } catch (const std::exception &e) {
    ctx.Error.fatal(std::string("Exception during block layout: ") + e.what() +
                    "\n(Likely an empty string passed to std::stoi)");
  }

  if (fn.is_async) {
    ctx.Builder->SetInsertPoint(ctx.CoroSuspendBlock);
    llvm::Function *endFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_end);
    llvm::Value *isUnwind = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
    llvm::Value *noneToken = llvm::ConstantTokenNone::get(ctx.Context);
    ctx.Builder->CreateCall(endFn, {ctx.CurrentCoroHandle, isUnwind, noneToken});

    // ctx.Builder->CreateRet(ctx.CurrentCoroHandle);
    llvm::Type *retTy = F->getReturnType();
    if (retTy->isStructTy()) {
      // Create a {ptr, i1} struct to represent the Future.
      llvm::Value *futureVal = llvm::UndefValue::get(retTy);

      // Insert the coroutine handle at index 0 (ptr)
      futureVal = ctx.Builder->CreateInsertValue(futureVal, ctx.CurrentCoroHandle, 0);

      // Insert 'false' (0) at index 1 (i1) to indicate the future is pending/suspended
      llvm::Value *isDone = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
      futureVal = ctx.Builder->CreateInsertValue(futureVal, isDone, 1);

      ctx.Builder->CreateRet(futureVal);
    } else {
      // Fallback if an async function ever returns void or just a raw pointer
      ctx.Builder->CreateRet(ctx.CurrentCoroHandle);
    }

    ctx.Builder->SetInsertPoint(ctx.CoroCleanupBlock);
    llvm::Function *freeFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_free);
    llvm::Value *memToFree = ctx.Builder->CreateCall(freeFn, {ctx.CoroId, ctx.CurrentCoroHandle});
    llvm::Function *mamlFreeFn = ctx.Module->getFunction("maml_free");
    ctx.Builder->CreateCall(mamlFreeFn, {memToFree});
    ctx.Builder->CreateBr(ctx.CoroSuspendBlock);
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
  for (const auto &fn : prog.functions) compileFunction(ctx, fn);

  // --- Final Module Verification ---
  if (llvm::verifyModule(*ctx.Module, &llvm::errs())) {
    ctx.Error.fatal("LLVM Module Verification failed! Check the console output above for details.");
  }
}

}  // namespace maml