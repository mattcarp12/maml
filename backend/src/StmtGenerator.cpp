#include "StmtGenerator.h"

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt) {
  if (stmt.is_null()) return;

  std::string_view nodeType = "unknown";
  if (stmt.contains("node_type")) {
    nodeType = stmt["node_type"].get<std::string_view>();
  }

  // ---------------------------------------------------------------------------
  // 1. Structural Instructions
  // ---------------------------------------------------------------------------

  if (nodeType == "TempDeclInst") {
    llvm::Type *ty = llvmTypeFor(ctx, stmt["maml_type"]);
    std::string name = stmt["name"].get<std::string>();

    // Allocate the temporary register on the stack
    llvm::AllocaInst *alloca = ctx.Builder->CreateAlloca(ty, nullptr, name);

    // Bind it to the flat function-level scope
    ctx.SymbolEnv.back()[name] = alloca;
    return;
  }

  if (nodeType == "AssignInst") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *targetPtr = ctx.resolveSymbol(dst);

    // Evaluate the flat, non-nested Right-Hand Side expression
    llvm::Value *rhsVal = evaluateExpression(ctx, stmt["expr"]);

    if (targetPtr && rhsVal) {
      ctx.Builder->CreateStore(rhsVal, targetPtr);
    } else {
      ctx.Error.fatal("AssignInst failed: target pointer or rhs value is null", stmt);
    }
    return;
  }

  // ---------------------------------------------------------------------------
  // Coroutine Initialization
  // ---------------------------------------------------------------------------

  if (nodeType == "CoroPrologueInst") {
    llvm::Value *align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
    llvm::Value *nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));

    // Create a basic 8-bit Promise object on the stack to hold the coroutine state
    llvm::Type *promiseTy = llvm::Type::getInt8Ty(ctx.Context);
    llvm::Value *promiseAlloc = ctx.Builder->CreateAlloca(promiseTy, nullptr, "promise");

    // 1. coro.id
    llvm::Function *coroIdFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_id);
    llvm::Value *coroId = ctx.Builder->CreateCall(coroIdFn, {align, promiseAlloc, nullPtr, nullPtr}, "coro.id");
    ctx.SymbolEnv.back()[std::string(rt::CORO_ID)] = coroId;

    // 2. coro.size
    llvm::Function *coroSizeFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_size,
                                                                 {llvm::Type::getInt64Ty(ctx.Context)});
    llvm::Value *coroSize = ctx.Builder->CreateCall(coroSizeFn, {}, "coro.size");

    // 3. Alloc Memory
    llvm::FunctionCallee mamlAlloc = ctx.Module->getOrInsertFunction(
        rt::ALLOC, llvm::FunctionType::get(llvm::PointerType::getUnqual(ctx.Context),
                                           {llvm::Type::getInt64Ty(ctx.Context)}, false));
    llvm::Value *allocMem = ctx.Builder->CreateCall(mamlAlloc, {coroSize}, "coro.alloc.mem");

    // 4. coro.begin
    llvm::Function *coroBeginFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_begin);
    llvm::Value *coroHdl = ctx.Builder->CreateCall(coroBeginFn, {coroId, allocMem}, "coro.hdl");
    ctx.SymbolEnv.back()[std::string(rt::CORO_HDL)] = coroHdl;
    return;
  }

  // ---------------------------------------------------------------------------
  // Explicit Memory Opcodes
  // ---------------------------------------------------------------------------

  if (nodeType == "CopyInst" || nodeType == "MoveInst") {
    std::string dst = stmt["dst"].get<std::string>();
    std::string src = stmt["src"].get<std::string>();
    llvm::Value *dstPtr = ctx.resolveSymbol(dst);

    if (!dstPtr) {
      ctx.Error.fatal("Move/Copy destination not found: " + dst, stmt);
      return;
    }

    // Determine the type of the destination
    llvm::Type *dstTy = nullptr;
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(dstPtr)) {
      dstTy = alloca->getAllocatedType();
    }

    llvm::Value *srcVal = nullptr;

    // Helper lambda to safely check if the string represents an integer
    auto isInteger = [](const std::string &s) {
      if (s.empty()) return false;
      size_t start = (s[0] == '-' || s[0] == '+') ? 1 : 0;
      if (start == s.length()) return false;  // Handle standalone "+" or "-"
      return std::all_of(s.begin() + start, s.end(), [](unsigned char c) { return std::isdigit(c); });
    };

    // Fast-path evaluation for flat literal sources
    if (src == "true") {
      srcVal = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1);
    } else if (src == "false") {
      srcVal = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
    } else if (src.find("zero_alloc") == 0) {
      srcVal = llvm::Constant::getNullValue(dstTy);
    } else if (isInteger(src)) {
      srcVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), std::stoi(src));
    } else {
      // It is a variable name. Resolve it and load the data.
      llvm::Value *srcPtr = ctx.resolveSymbol(src);
      if (srcPtr) {
        llvm::Type *srcTy = dstTy;
        if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(srcPtr)) {
          srcTy = alloca->getAllocatedType();
        }
        srcVal = ctx.Builder->CreateLoad(srcTy, srcPtr, src + "_val");
      }
    }

    if (srcVal && !srcVal->getType()->isVoidTy()) {
      ctx.Builder->CreateStore(srcVal, dstPtr);
    } else if (!srcVal) {
      ctx.Error.fatal("Move/Copy source invalid or undefined: " + src, stmt);
    }
    return;
  }

  if (nodeType == "RefAllocInst") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Type *valTy = llvmTypeFor(ctx, stmt["maml_type"]);
    llvm::Value *dstPtr = ctx.resolveSymbol(dst);

    if (!dstPtr) return;

    // Calculate exact byte size needed for the struct/array
    llvm::DataLayout DL(ctx.Module.get());
    uint64_t allocSize = DL.getTypeAllocSize(valTy);

    // Invoke maml_alloc(size)
    llvm::FunctionCallee mamlAlloc = ctx.Module->getOrInsertFunction(
        rt::ALLOC, llvm::FunctionType::get(llvm::PointerType::getUnqual(ctx.Context),
                                           {llvm::Type::getInt64Ty(ctx.Context)}, false));

    llvm::Value *heapPtr = ctx.Builder->CreateCall(
        mamlAlloc, {llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), allocSize)}, "heap_alloc");

    ctx.Builder->CreateStore(heapPtr, dstPtr);
    return;
  }

  if (nodeType == "RefIncInst" || nodeType == "RefDecInst") {
    std::string src = stmt["src"].get<std::string>();
    llvm::Value *srcPtr = ctx.resolveSymbol(src);
    if (!srcPtr) return;

    // The local variable holds a heap pointer; we must load that pointer to pass it to the runtime
    llvm::Type *ptrTy = llvm::PointerType::getUnqual(ctx.Context);
    llvm::Value *heapPtr = ctx.Builder->CreateLoad(ptrTy, srcPtr, src + "_load");

    const char *hook = (nodeType == "RefIncInst") ? rt::RETAIN : rt::RELEASE;
    llvm::FunctionCallee refFn = ctx.Module->getOrInsertFunction(
        hook, llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context), {ptrTy}, false));

    ctx.Builder->CreateCall(refFn, {heapPtr});
    return;
  }

  if (nodeType == "MutBorrowInst") {
    // Affine uniqueness and mutable borrow exclusivity are mathematically proven
    // by the frontend SEMA and Ownership passes.
    // This is purely a compile-time assertion, so it compiles to a zero-cost runtime no-op!
    return;
  }

  ctx.Error.fatal("Unsupported MIR Instruction encountered: " + std::string(nodeType), stmt);
}

void compileTerminator(CodegenContext &ctx, const nlohmann::json &term) {
  if (term.is_null()) {
    ctx.Builder->CreateUnreachable();
    return;
  }

  std::string_view nodeType = term["node_type"].get<std::string_view>();

  // --- RETURN TERMINATOR ---
  if (nodeType == "ReturnTerminator") {
    // Coroutine early-out: suspend points handle ARC, just return the handle.
    if (llvm::Value *coroHdl = ctx.resolveSymbol(rt::CORO_HDL)) {
      ctx.Builder->CreateRet(coroHdl);
      return;
    }

    llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
    llvm::Type *retTy = F->getReturnType();

    if (retTy->isVoidTy()) {
      ctx.Builder->CreateRetVoid();
    } else {
      llvm::Value *retPtr = ctx.resolveSymbol("_ret");
      if (retPtr) {
        llvm::Value *retVal = ctx.Builder->CreateLoad(retTy, retPtr, "ret_load");
        ctx.Builder->CreateRet(retVal);
      } else {
        // Failsafe for uninitialized paths
        ctx.Builder->CreateRet(llvm::Constant::getNullValue(retTy));
      }
    }
    return;
  }

  // --- UNCONDITIONAL JUMP ---
  if (nodeType == "JumpTerminator") {
    int target = term["target"].get<int>();
    ctx.Builder->CreateBr(ctx.Blocks[target]);
    return;
  }

  // --- CONDITIONAL BRANCH ---
  if (nodeType == "BranchTerminator") {
    llvm::Value *cond = evaluateExpression(ctx, term["condition"]);
    int trueTarget = term["true_target"].get<int>();
    int falseTarget = term["false_target"].get<int>();
    ctx.Builder->CreateCondBr(cond, ctx.Blocks[trueTarget], ctx.Blocks[falseTarget]);
    return;
  }

  // --- ASYNC SUSPENSION BOUNDARY ---
  if (nodeType == "CoroSuspendTerminator") {
    int resumeTarget = term["resume_target"].get<int>();
    int cleanupTarget = term["cleanup_target"].get<int>();
    int suspendTarget = term["suspend_target"].get<int>();

    llvm::Function *coroSuspendFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_suspend);
    llvm::Value *noneToken = llvm::ConstantTokenNone::get(ctx.Context);
    llvm::Value *isFinalValue = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);

    // Call the intrinsic which returns an i8 routing byte
    llvm::Value *suspendResult = ctx.Builder->CreateCall(coroSuspendFn, {noneToken, isFinalValue}, "suspend_res");

    // Build a transparent trampoline block to execute coro.free on the cleanup path.
    // This allows Go to remain "dumb" about LLVM's internal memory management.
    llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock *trampolineBB = llvm::BasicBlock::Create(ctx.Context, "coro.cleanup.trampoline", parentFn);

    // 3-Way Control Flow Split
    llvm::SwitchInst *sw = ctx.Builder->CreateSwitch(suspendResult, ctx.Blocks[suspendTarget], 2);
    sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 0), ctx.Blocks[resumeTarget]);
    sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 1), trampolineBB);

    // Populate the Trampoline
    ctx.Builder->SetInsertPoint(trampolineBB);
    llvm::Function *coroFreeFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_free);

    llvm::Value *cId = ctx.resolveSymbol(rt::CORO_ID);
    llvm::Value *cHdl = ctx.resolveSymbol(rt::CORO_HDL);
    if (cId && cHdl) {
      ctx.Builder->CreateCall(coroFreeFn, {cId, cHdl});
    }

    // Jump to the MIR's explicitly defined cleanup path
    ctx.Builder->CreateBr(ctx.Blocks[cleanupTarget]);
    return;
  }

  // --- UNREACHABLE ---
  if (nodeType == "UnreachableTerminator") {
    ctx.Builder->CreateUnreachable();
    return;
  }

  ctx.Error.fatal("Unsupported MIR Terminator encountered: " + std::string(nodeType), term);
}

}  // namespace maml