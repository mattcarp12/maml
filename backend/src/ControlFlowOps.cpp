#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

void handle(CodegenContext &ctx, const mir::BinaryOpInst &inst) {
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
  llvm::Value *operand = evaluateValue(ctx, inst.operand);
  llvm::Value *result = nullptr;

  if (inst.operator_ == "!") {
    result = ctx.Builder->CreateXor(operand, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), "nottmp");
  } else if (inst.operator_ == "-") {
    result = ctx.Builder->CreateSub(llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0), operand, "negtmp");
  }
  ctx.SymbolEnv.back()[inst.dst] = result;
}

void handle(CodegenContext &ctx, const mir::CallInst &inst) {
  llvm::Value *callee = evaluateValue(ctx, inst.function);

  llvm::FunctionType *FT = nullptr;
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    FT = F->getFunctionType();
  } else {
    llvm::Type *expectedRetTy = llvmTypeFor(ctx, inst.type);
    std::vector<llvm::Type *> expectedArgTys;
    for (const auto &argWrapper : inst.arguments) {
      // Note: Since mir::Value doesn't explicitly store its type natively,
      // you may need to fetch types from the context or pass typed arguments.
      // For now, we dynamically get it from the evaluated value:
      llvm::Value *tmp = evaluateValue(ctx, argWrapper.argument);
      expectedArgTys.push_back(tmp->getType());
    }
    FT = llvm::FunctionType::get(expectedRetTy, expectedArgTys, false);
  }

  std::vector<llvm::Value *> args;
  size_t i = 0;

  // Resolve name from callee if possible to support map/vec hooks
  std::string funcName = "";
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    funcName = F->getName().str();
  }

  for (const auto &argWrapper : inst.arguments) {
    llvm::Value *argVal = evaluateValue(ctx, argWrapper.argument);

    if (funcName == rt::MAP_CREATE) {
      if (i == 0) {
        int itemSize = 4;
        if (inst.type.contains("value")) {
          const auto &valType = inst.type["value"];
          if (valType.is_object() && valType.contains("size"))
            itemSize = valType["size"].get<int>();
          else
            itemSize = llvm::DataLayout(ctx.Module.get()).getTypeAllocSize(llvmTypeFor(ctx, valType));
        }
        argVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), itemSize);
      } else if (i == 1) {
        bool isStr = inst.type.contains("key") && inst.type["key"].get<std::string_view>() == "string";
        argVal = llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), isStr ? 1 : 0);
      }
    } else if (funcName == rt::VEC_CREATE) {
      if (i == 0) {
        int itemSize = 4;
        if (inst.type.contains("base")) {
          const auto &elemType = inst.type["base"];
          if (elemType.is_object() && elemType.contains("size"))
            itemSize = elemType["size"].get<int>();
          else
            itemSize = llvm::DataLayout(ctx.Module.get()).getTypeAllocSize(llvmTypeFor(ctx, elemType));
        }
        argVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), itemSize);
      }
    }

    if (FT && i < FT->getNumParams()) {
      llvm::Type *expectedTy = FT->getParamType(i);
      llvm::Type *actualTy = argVal->getType();
      if (expectedTy != actualTy) {
        if (expectedTy->isPointerTy() && actualTy->isStructTy()) {
          auto *structTy = llvm::dyn_cast<llvm::StructType>(actualTy);
          if (structTy && structTy->isLiteral() && funcName.compare(0, 9, "maml_vec_") != 0 &&
              funcName.compare(0, 9, "maml_map_") != 0) {
            argVal = ctx.Builder->CreateExtractValue(argVal, {0}, "fat_ptr_unwrap");
          } else {
            llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
            llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
            llvm::AllocaInst *spill = TmpBuilder.CreateAlloca(actualTy, nullptr, "arg_struct_spill");
            ctx.Builder->CreateStore(argVal, spill);
            argVal = spill;
          }
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
        }
      }
    }
    args.push_back(argVal);
    i++;
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