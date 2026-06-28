#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.hpp"
#include "RuntimeConstants.h"
#include "TypeLowering.hpp"

namespace maml {

void handle(CodegenContext &ctx, const mir::BinaryOpInst &inst) {
  ctx.CurrentInstructionName = "BinaryOpInst (" + inst.operator_ + ")";

  llvm::Value *left = evaluateValue(ctx, inst.left);
  llvm::Value *right = evaluateValue(ctx, inst.right);
  llvm::Value *result = nullptr;

  if (left->getType()->isPointerTy() && right->getType()->isIntegerTy()) {
    if (auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(right); cInt && cInt->isZero())
      right = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(left->getType()));
  } else if (right->getType()->isPointerTy() && left->getType()->isIntegerTy()) {
    if (auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(left); cInt && cInt->isZero())
      left = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(right->getType()));
  }

  if (inst.operator_ == "/" || inst.operator_ == "%") {
    llvm::Value *isZero = ctx.Builder->CreateICmpEQ(right, llvm::ConstantInt::get(right->getType(), 0));
    llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock *trapBB = llvm::BasicBlock::Create(ctx.Context, "trap_div_zero", F);
    llvm::BasicBlock *contBB = llvm::BasicBlock::Create(ctx.Context, "cont_div", F);

    ctx.Builder->CreateCondBr(isZero, trapBB, contBB);
    ctx.Builder->SetInsertPoint(trapBB);
    llvm::Function *trapFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::trap);
    ctx.Builder->CreateCall(trapFn);
    ctx.Builder->CreateUnreachable();

    ctx.Builder->SetInsertPoint(contBB);
    if (inst.operator_ == "/") result = ctx.Builder->CreateSDiv(left, right, "divtmp");
    if (inst.operator_ == "%") result = ctx.Builder->CreateSRem(left, right, "modtmp");
  } else if (inst.operator_ == "+")
    result = ctx.Builder->CreateAdd(left, right, "addtmp");
  else if (inst.operator_ == "-")
    result = ctx.Builder->CreateSub(left, right, "subtmp");
  else if (inst.operator_ == "*")
    result = ctx.Builder->CreateMul(left, right, "multmp");
  else if (inst.operator_ == "==")
    result = ctx.Builder->CreateICmpEQ(left, right, "eqtmp");
  else if (inst.operator_ == "!=")
    result = ctx.Builder->CreateICmpNE(left, right, "neqtmp");
  else if (inst.operator_ == "<")
    result = ctx.Builder->CreateICmpSLT(left, right, "lttmp");
  else if (inst.operator_ == ">")
    result = ctx.Builder->CreateICmpSGT(left, right, "gttmp");
  else if (inst.operator_ == "<=")
    result = ctx.Builder->CreateICmpSLE(left, right, "letmp");
  else if (inst.operator_ == ">=")
    result = ctx.Builder->CreateICmpSGE(left, right, "getmp");

  if (llvm::Value *existing = ctx.resolveSymbol(inst.dst)) {
    if (llvm::isa<llvm::AllocaInst>(existing)) {
      ctx.Builder->CreateStore(result, existing);
    } else {
      ctx.SymbolEnv.back()[inst.dst] = result;
    }
  } else {
    ctx.SymbolEnv.back()[inst.dst] = result;
  }
}

void handle(CodegenContext &ctx, const mir::UnaryOpInst &inst) {
  ctx.CurrentInstructionName = "UnaryOpInst (" + inst.operator_ + ")";

  llvm::Value *operand = evaluateValue(ctx, inst.operand);
  llvm::Value *result = nullptr;

  if (inst.operator_ == "!") {
    result = ctx.Builder->CreateXor(operand, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), "nottmp");
  } else if (inst.operator_ == "-") {
    result = ctx.Builder->CreateSub(llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0), operand, "negtmp");
  }
  ctx.SymbolEnv.back()[inst.dst] = result;
}

static void lowerTaskGetResult(CodegenContext &ctx, const mir::CallInst &inst) {
  llvm::Value *hdl = evaluateValue(ctx, inst.arguments[0]);

  llvm::Function *promiseFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_promise);
  llvm::Value *align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 8);
  llvm::Value *from = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
  llvm::Value *promisePtr = ctx.Builder->CreateCall(promiseFn, {hdl, align, from});

  llvm::Type *expectedTy = llvmTypeFor(ctx, inst.type);

  if (!expectedTy->isVoidTy()) {
    llvm::Value *typedPromise = ctx.Builder->CreatePointerCast(promisePtr, llvm::PointerType::getUnqual(expectedTy));
    llvm::Value *res = ctx.Builder->CreateLoad(expectedTy, typedPromise, "coro.result");

    if (llvm::Value *existing = ctx.resolveSymbol(inst.dst)) {
      if (llvm::isa<llvm::AllocaInst>(existing)) {
        ctx.Builder->CreateStore(res, existing);
      } else {
        ctx.SymbolEnv.back()[inst.dst] = res;
      }
    } else {
      ctx.SymbolEnv.back()[inst.dst] = res;
    }
  }
}

static std::vector<llvm::Value *> prepareCallArguments(CodegenContext &ctx, const mir::CallInst &inst,
                                                       llvm::FunctionType *FT, const std::string &funcName) {
  std::vector<llvm::Value *> args;
  size_t i = 0;

  for (const auto &argWrapper : inst.arguments) {
    llvm::Value *argVal = evaluateValue(ctx, argWrapper);

    if (FT && i < FT->getNumParams()) {
      llvm::Type *expectedTy = FT->getParamType(i);
      llvm::Type *actualTy = argVal->getType();

      if (expectedTy != actualTy) {
        if (expectedTy->isPointerTy() && actualTy->isStructTy()) {
          llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
          llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
          llvm::AllocaInst *spill = TmpBuilder.CreateAlloca(actualTy, nullptr, "arg_struct_spill");
          ctx.Builder->CreateStore(argVal, spill);
          argVal = spill;
        } else if (expectedTy->isIntegerTy() && actualTy->isIntegerTy()) {
          argVal = ctx.Builder->CreateIntCast(argVal, expectedTy, false, "arg_cast");
        } else if (expectedTy->isPointerTy() && actualTy->isPointerTy()) {
          argVal = ctx.Builder->CreatePointerCast(argVal, expectedTy, "ptr_cast");
        } else if (expectedTy->isPointerTy() && !actualTy->isPointerTy()) {
          auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(argVal);
          if (cInt && cInt->isZero() && actualTy->isIntegerTy(64)) {
            argVal = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(expectedTy));
          } else {
            llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
            llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
            llvm::AllocaInst *spill = TmpBuilder.CreateAlloca(actualTy, nullptr, "arg_spill");
            ctx.Builder->CreateStore(argVal, spill);
            argVal = spill;
          }
        } else {
          // --- Strict Observability Check ---
          // If we exhaust all safe coercion techniques, halt the compiler nicely!
          std::string errMsg = "Type mismatch for argument " + std::to_string(i) + " in " + funcName + ".\n" +
                               "     Expected: " + maml::ErrorHandler::stringify(expectedTy) + "\n" +
                               "     Got:      " + maml::ErrorHandler::stringify(actualTy) + "\n" +
                               "     Value:    " + maml::ErrorHandler::stringify(argVal);
          ctx.Error.fatal(errMsg);
        }
      }
    }
    args.push_back(argVal);
    i++;
  }
  return args;
}

void handle(CodegenContext &ctx, const mir::CallInst &inst) {
  llvm::Value *callee = evaluateValue(ctx, inst.function);

  std::string funcName = "";
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    funcName = F->getName().str();
  } else if (auto *reg = std::get_if<mir::Register>(&inst.function.inner)) {
    funcName = reg->name;
  }

  ctx.CurrentInstructionName = "CallInst (" + funcName + ")";

  if (funcName == maml::rt::TASK_GET_RESULT) {
    lowerTaskGetResult(ctx, inst);
    return;
  }

  llvm::FunctionType *FT = nullptr;
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    FT = F->getFunctionType();
  } else {
    llvm::Type *expectedRetTy = llvmTypeFor(ctx, inst.type);
    std::vector<llvm::Type *> expectedArgTys;
    for (const auto &argWrapper : inst.arguments) {
      expectedArgTys.push_back(evaluateValue(ctx, argWrapper)->getType());
    }
    FT = llvm::FunctionType::get(expectedRetTy, expectedArgTys, false);
  }

  std::vector<llvm::Value *> args = prepareCallArguments(ctx, inst, FT, funcName);

  if (funcName == maml::rt::TASK_AWAIT || funcName == maml::rt::YIELD_NOW) {
    args.push_back(ctx.CurrentCoroHandle);
  }

  llvm::CallInst *callResult = FT && FT->getReturnType()->isVoidTy()
                                   ? ctx.Builder->CreateCall(FT, callee, args)
                                   : ctx.Builder->CreateCall(FT, callee, args, "calltmp");

  if (!callResult->getType()->isVoidTy()) {
    llvm::Type *expectedRetTy = llvmTypeFor(ctx, inst.type);
    llvm::Value *finalResult = callResult;

    if (callResult->getType() != expectedRetTy) {
      if (callResult->getType()->isIntegerTy() && expectedRetTy->isIntegerTy()) {
        finalResult = ctx.Builder->CreateIntCast(callResult, expectedRetTy, true, "call_ret_cast");
      } else if (callResult->getType()->isPointerTy() && expectedRetTy->isStructTy()) {
        finalResult = callResult;
      }
    }

    if (llvm::Value *existing = ctx.resolveSymbol(inst.dst)) {
      if (llvm::isa<llvm::AllocaInst>(existing)) {
        ctx.Builder->CreateStore(finalResult, existing);
      } else {
        ctx.SymbolEnv.back()[inst.dst] = finalResult;
      }
    } else {
      ctx.SymbolEnv.back()[inst.dst] = finalResult;
    }
  }
}

}  // namespace maml