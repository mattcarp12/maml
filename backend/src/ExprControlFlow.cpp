#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"

namespace maml {

llvm::Value *compileAsyncPrologueExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  auto &Context = ctx.Context;
  auto &Module = ctx.Module;

  llvm::Value *align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
  llvm::Value *nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));

  // Create a basic 8-bit Promise object on the stack to hold the coroutine return state
  llvm::Type *promiseTy = llvm::Type::getInt8Ty(Context);
  llvm::Value *promiseAlloc = Builder->CreateAlloca(promiseTy, nullptr, "promise");
  llvm::Value *promisePtr = Builder->CreateBitCast(promiseAlloc, llvm::PointerType::getUnqual(Context));

  llvm::Function *coroIdFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_id);
  llvm::Value *coroId = Builder->CreateCall(coroIdFn, {align, promisePtr, nullPtr, nullPtr}, "coro.id");
  ctx.SymbolTable[rt::CORO_ID] = coroId;

  llvm::Function *coroSizeFn =
      llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_size, {llvm::Type::getInt64Ty(Context)});
  llvm::Value *coroSize = Builder->CreateCall(coroSizeFn, {}, "coro.size");

  llvm::FunctionCallee mamlAlloc = Module->getOrInsertFunction(
      rt::ALLOC,
      llvm::FunctionType::get(llvm::PointerType::getUnqual(Context), {llvm::Type::getInt64Ty(Context)}, false));
  llvm::Value *allocMem = Builder->CreateCall(mamlAlloc, {coroSize}, "coro.alloc.mem");

  llvm::Function *coroBeginFn = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_begin);
  llvm::Value *coroHdl = Builder->CreateCall(coroBeginFn, {coroId, allocMem}, "coro.hdl");
  ctx.SymbolTable[rt::CORO_HDL] = coroHdl;

  return coroHdl;  // This evaluates to the Task<T> handle!
}

}  // namespace maml