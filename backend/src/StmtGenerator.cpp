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

    // 1. Get the current function and its entry block
    llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock &entryBlock = parentFn->getEntryBlock();

    // 2. Create a temporary builder pointing to the top of the entry block
    llvm::IRBuilder<> TmpBuilder(&entryBlock, entryBlock.begin());

    // 3. Emit the AllocaInst safely at the entry block
    llvm::AllocaInst *alloca = TmpBuilder.CreateAlloca(ty, nullptr, name);

    // 4. Bind it to the flat function-level scope
    ctx.SymbolEnv.back()[name] = alloca;
    return;
  }

  if (nodeType == "AssignInst") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Value *targetPtr = ctx.resolveSymbol(dst);  // This is an AllocaInst* (pointer)

    // Evaluate the flat, non-nested Right-Hand Side expression
    llvm::Value *rhsVal = evaluateExpression(ctx, stmt["expr"]);

    if (targetPtr && rhsVal) {
      llvm::Type *targetTy = nullptr;

      // Attempt to extract the type from the stack allocation
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetPtr)) {
        targetTy = alloca->getAllocatedType();
      }
      // FIXED: Fallback for heap-allocated opaque pointers (RefAllocInst)
      else if (stmt.contains("expr") && stmt["expr"].contains("maml_type") && !stmt["expr"]["maml_type"].is_null()) {
        targetTy = llvmTypeFor(ctx, stmt["expr"]["maml_type"]);
      }

      // Check if the target is an aggregate type (Struct, String, or Array)
      if (targetTy && (targetTy->isStructTy() || targetTy->isArrayTy())) {
        // CASE 1: RHS is an aggregate pointer (like a variable reference). We must memcpy.
        if (rhsVal->getType()->isPointerTy()) {
          llvm::DataLayout DL(ctx.Module.get());
          uint64_t sizeBytes = DL.getTypeAllocSize(targetTy);

          ctx.Builder->CreateMemCpy(targetPtr, llvm::MaybeAlign(), rhsVal, llvm::MaybeAlign(), sizeBytes);
        }
        // CASE 2: RHS is a first-class aggregate value (like a ConstantStruct literal). Store directly!
        else {
          ctx.Builder->CreateStore(rhsVal, targetPtr);
        }
      } else {
        // Standard primitive path
        ctx.Builder->CreateStore(rhsVal, targetPtr);
      }
    }
    return;
  }

  if (nodeType == "IndexAssignInst") {
    std::string target = stmt["target"].get<std::string>();
    llvm::Value *targetPtr = ctx.resolveSymbol(target);
    llvm::Value *indexVal = evaluateExpression(ctx, stmt["index"]);
    llvm::Value *rhsVal = evaluateExpression(ctx, stmt["value"]);

    if (!targetPtr) return;

    // Resolve the actual element type from the JSON expression
    llvm::Type *elemTy = nullptr;
    if (stmt.contains("value") && stmt["value"].contains("maml_type")) {
      elemTy = llvmTypeFor(ctx, stmt["value"]["maml_type"]);
    } else if (!rhsVal->getType()->isPointerTy()) {
      elemTy = rhsVal->getType();
    } else {
      ctx.Error.fatal("IndexSetInst requires type metadata for opaque pointers", stmt);
      return;
    }

    llvm::Type *targetTy = nullptr;
    if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetPtr)) {
      targetTy = alloca->getAllocatedType();
    }

    llvm::Value *elemPtr = nullptr;
    if (targetTy && targetTy->isArrayTy()) {
      elemPtr = ctx.Builder->CreateGEP(targetTy, targetPtr, {ctx.Builder->getInt32(0), indexVal}, "array_idx_set");
    } else if (targetTy) {
      // It is a slice. Load the heap data pointer from index 1.
      llvm::Value *dataPtr =
          ctx.Builder->CreateGEP(targetTy, targetPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(1)});
      llvm::Value *data = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), dataPtr);
      // GEP using the explicit element type
      elemPtr = ctx.Builder->CreateGEP(elemTy, data, indexVal, "slice_idx_set");
    }

    // Safely route memory copying based on whether RHS is a pointer or a direct value
    if (elemTy && (elemTy->isStructTy() || elemTy->isArrayTy())) {
      if (rhsVal->getType()->isPointerTy()) {
        llvm::DataLayout DL(ctx.Module.get());
        uint64_t sizeBytes = DL.getTypeAllocSize(elemTy);
        ctx.Builder->CreateMemCpy(elemPtr, llvm::MaybeAlign(), rhsVal, llvm::MaybeAlign(), sizeBytes);
      } else {
        ctx.Builder->CreateStore(rhsVal, elemPtr);
      }
    } else {
      ctx.Builder->CreateStore(rhsVal, elemPtr);
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

  // ===========================================================================
  // Unified ARC Handlers
  // ===========================================================================
  if (nodeType == "RefIncInst" || nodeType == "RefDecInst") {
    std::string src = stmt["src"].get<std::string>();
    llvm::Value *targetPtr = ctx.resolveSymbol(src);
    if (!targetPtr) return;

    llvm::Value *opPtr = nullptr;

    // 1. Handle Escaped Heap Variables (RefAllocInst)
    if (llvm::isa<llvm::CallInst>(targetPtr)) {
      // The variable itself was dynamically allocated.
      // We directly manage its root heap block pointer.
      opPtr = targetPtr;
    }
    // 2. Handle Local Stack Variables (TempDeclInst)
    else if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetPtr)) {
      llvm::Type *allocTy = alloca->getAllocatedType();

      // Unpack the heap reference pointer from slice/string/array aggregates
      if (allocTy->isStructTy() || allocTy->isArrayTy()) {
        llvm::Value *gep = ctx.Builder->CreateGEP(allocTy, targetPtr,
                                                  {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)}, "heap_ptr_gep");
        opPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), gep, "heap_ptr_load");
      } else {
        opPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), targetPtr, "ptr_load");
      }
    }
    // 3. Fallback for Opaque Pointers and Function Arguments
    else {
      opPtr = ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context), targetPtr, "ptr_load");
    }

    // Route to the appropriate Zig runtime hook
    llvm::Function *arcFn = ctx.Module->getFunction(nodeType == "RefIncInst" ? maml::rt::RETAIN : maml::rt::RELEASE);

    if (arcFn) {
      ctx.Builder->CreateCall(arcFn, {opPtr});
    }
    return;
  }

  if (nodeType == "RefAllocInst") {
    std::string dst = stmt["dst"].get<std::string>();
    llvm::Type *valTy = llvmTypeFor(ctx, stmt["maml_type"]);

    // 1. Create the local stack variable to hold the reference
    llvm::AllocaInst *dstAlloca = ctx.Builder->CreateAlloca(valTy, nullptr, dst);

    // 2. Register the local variable in the current scope
    ctx.SymbolEnv.back()[dst] = dstAlloca;

    // 3. Calculate the required heap allocation size in bytes
    llvm::DataLayout DL(ctx.Module.get());
    uint64_t size = DL.getTypeAllocSize(valTy);
    llvm::Value *sizeVal = llvm::ConstantInt::get(llvm::Type::getInt64Ty(ctx.Context), size);

    // 4. Safely get or insert the declaration for maml_alloc
    llvm::FunctionCallee allocFn = ctx.Module->getOrInsertFunction(
        rt::ALLOC, llvm::FunctionType::get(llvm::PointerType::getUnqual(ctx.Context),
                                           {llvm::Type::getInt64Ty(ctx.Context)}, false));

    // 5. Emit the heap allocation call
    llvm::Value *heapPtr = ctx.Builder->CreateCall(allocFn, {sizeVal}, dst + "_heap_alloc");

    // 6. Store the heap pointer into the local stack variable
    ctx.Builder->CreateStore(heapPtr, dstAlloca);

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