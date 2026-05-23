#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"
#include <llvm/IR/Intrinsics.h>

namespace maml {

llvm::Value *compileIfExpr(CodegenContext &ctx, const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *thenBB = llvm::BasicBlock::Create(ctx.Context, "then", F);
  llvm::BasicBlock *elseBB = llvm::BasicBlock::Create(ctx.Context, "else", F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(ctx.Context, "merge", F);

  llvm::Value *cond = evaluateExpression(ctx, e["condition"]);
  Builder->CreateCondBr(cond, thenBB, elseBB);

  llvm::Type *expectedTy = nullptr;
  if (e.contains("maml_type") && e["maml_type"]["kind"] != "Unit" &&
      e["maml_type"]["kind"] != "Unknown") {
    expectedTy = llvmTypeFor(ctx, e["maml_type"]);
  }

  Builder->SetInsertPoint(thenBB);
  llvm::Value *thenVal = evaluateExpression(ctx, e["consequence"]);

  if (expectedTy && thenVal && thenVal->getType()->isPointerTy() &&
      !expectedTy->isPointerTy()) {
    thenVal = Builder->CreateLoad(expectedTy, thenVal, "then_load");
  }
  if (!Builder->GetInsertBlock()->getTerminator())
    Builder->CreateBr(mergeBB);
  thenBB = Builder->GetInsertBlock();

  Builder->SetInsertPoint(elseBB);
  llvm::Value *elseVal = nullptr;
  if (e.contains("alternative")) {
    elseVal = evaluateExpression(ctx, e["alternative"]);
  }

  if (expectedTy && elseVal && elseVal->getType()->isPointerTy() &&
      !expectedTy->isPointerTy()) {
    elseVal = Builder->CreateLoad(expectedTy, elseVal, "else_load");
  }
  if (!elseVal && expectedTy)
    elseVal = llvm::Constant::getNullValue(expectedTy);
  if (!Builder->GetInsertBlock()->getTerminator())
    Builder->CreateBr(mergeBB);
  elseBB = Builder->GetInsertBlock();

  Builder->SetInsertPoint(mergeBB);
  if (expectedTy && thenVal && elseVal) {
    llvm::PHINode *phi = Builder->CreatePHI(expectedTy, 2, "iftmp");
    phi->addIncoming(thenVal, thenBB);
    phi->addIncoming(elseVal, elseBB);
    return phi;
  }
  return nullptr;
}

llvm::Value *compilePatternCheck(CodegenContext &ctx,
                                 const nlohmann::json &pattern,
                                 llvm::Value *subject,
                                 const nlohmann::json &subjectType) {
  auto &Builder = ctx.Builder;
  std::string_view kind = pattern["kind"].get<std::string_view>();

  if (kind == "Wildcard")
    return llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1);

  if (kind == "Literal") {
    llvm::Type *valType = llvmTypeFor(ctx, pattern["value"]["maml_type"]);
    llvm::Value *subjectVal =
        Builder->CreateLoad(valType, subject, "match.lit.load");
    llvm::Value *litVal = evaluateExpression(ctx, pattern["value"]);
    return Builder->CreateICmpEQ(subjectVal, litVal, "match.lit.cmp");
  }
  if (kind == "Variant") {
    llvm::Type *sumTy = llvmTypeFor(ctx, subjectType);
    llvm::Value *discrimPtr = Builder->CreateGEP(
        sumTy, subject,
        {llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0),
         llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0)},
        "match.discrim.ptr");
    llvm::Value *actualDiscrim = Builder->CreateLoad(
        llvm::Type::getInt32Ty(ctx.Context), discrimPtr, "match.discrim.val");

    std::string_view variantName = pattern["name"].get<std::string_view>();
    int expected = 0;
    for (const auto &v : subjectType["variants"]) {
      if (v["name"].get<std::string_view>() == variantName) {
        expected = v["discriminant"];
        break;
      }
    }
    llvm::Value *expectedDiscrim =
        llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), expected);
    return Builder->CreateICmpEQ(actualDiscrim, expectedDiscrim,
                                 "match.discrim.cmp");
  }
  return llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
}

void injectPatternBindings(CodegenContext &ctx, const nlohmann::json &pattern,
                           llvm::Value *subject,
                           const nlohmann::json &subjectType) {
  auto &Builder = ctx.Builder;
  if (pattern["kind"] != "Variant")
    return;

  std::string_view variantName = pattern["name"].get<std::string_view>();
  nlohmann::json variantDef;
  for (const auto &v : subjectType["variants"]) {
    if (v["name"].get<std::string_view>() == variantName) {
      variantDef = v;
      break;
    }
  }
  if (variantDef["fields"].empty())
    return;

  llvm::Type *sumTy = llvmTypeFor(ctx, subjectType);
  llvm::Value *payloadPtr = Builder->CreateGEP(
      sumTy, subject, {Builder->getInt32(0), Builder->getInt32(1)},
      "payload_raw_ptr");

  std::vector<llvm::Type *> payloadTypes;
  for (const auto &f : variantDef["fields"])
    payloadTypes.push_back(llvmTypeFor(ctx, f["type"]));
  llvm::StructType *payloadStructTy =
      llvm::StructType::get(ctx.Context, payloadTypes);

  // payloadPtr is already an opaque ptr from CreateGEP — no cast needed.
  if (pattern.contains("binding")) {
    std::string bindName(pattern["binding"].get<std::string_view>());
    llvm::Value *fieldPtr = Builder->CreateGEP(
        payloadStructTy, payloadPtr,
        {Builder->getInt32(0), Builder->getInt32(0)}, "single_bind_ptr");
    ctx.SymbolEnv.back()[bindName] = fieldPtr;
  }

  if (pattern.contains("fields") && pattern["fields"].is_array()) {
    for (const auto &fb : pattern["fields"]) {
      std::string_view fieldName = fb["field"].get<std::string_view>();
      std::string bindName(fb["binding"].get<std::string_view>());
      int index = -1;
      for (int i = 0; i < (int)variantDef["fields"].size(); ++i) {
        if (variantDef["fields"][i]["name"].get<std::string_view>() ==
            fieldName) {
          index = i;
          break;
        }
      }
      if (index >= 0) {
        llvm::Value *fieldPtr = Builder->CreateGEP(
            payloadStructTy, payloadPtr,
            {Builder->getInt32(0), Builder->getInt32(index)}, "field_bind_ptr");
        ctx.SymbolEnv.back()[bindName] = fieldPtr;
      }
    }
  }
}

llvm::Value *compileMatchExpr(CodegenContext &ctx, const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Value *subject = evaluateExpression(ctx, e["subject"]);
  nlohmann::json subjectType = e["subject"]["maml_type"];

  llvm::Value *subjectPtr = subject;
  if (subject && !subject->getType()->isPointerTy()) {
    llvm::Type *sumTy = llvmTypeFor(ctx, subjectType);
    llvm::AllocaInst *spill =
        Builder->CreateAlloca(sumTy, nullptr, "match_subject_spill");
    Builder->CreateStore(subject, spill);
    subjectPtr = spill;
  }

  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *mergeBB =
      llvm::BasicBlock::Create(ctx.Context, "match_merge", F);
  std::vector<std::pair<llvm::Value *, llvm::BasicBlock *>> incomings;

  llvm::Type *expectedTy = nullptr;
  if (e.contains("maml_type") && e["maml_type"]["kind"] != "Unit" &&
      e["maml_type"]["kind"] != "Unknown") {
    expectedTy = llvmTypeFor(ctx, e["maml_type"]);
  }

  for (const auto &arm : e["arms"]) {
    llvm::BasicBlock *armBB =
        llvm::BasicBlock::Create(ctx.Context, "match_arm", F);
    llvm::BasicBlock *nextBB =
        llvm::BasicBlock::Create(ctx.Context, "match_next", F);
    llvm::Value *isMatch =
        compilePatternCheck(ctx, arm["pattern"], subjectPtr, subjectType);

    if (auto *constMatch = llvm::dyn_cast<llvm::ConstantInt>(isMatch)) {
      if (constMatch->isOne()) {
        Builder->CreateBr(armBB);
        nextBB->eraseFromParent();
        Builder->SetInsertPoint(armBB);

        std::vector<std::string> injectedKeys;
        if (arm["pattern"]["kind"] == "Variant") {
          if (arm["pattern"].contains("binding"))
            injectedKeys.push_back(
                std::string(arm["pattern"]["binding"].get<std::string_view>()));
          if (arm["pattern"].contains("fields") &&
              arm["pattern"]["fields"].is_array()) {
            for (const auto &fb : arm["pattern"]["fields"])
              injectedKeys.push_back(
                  std::string(fb["binding"].get<std::string_view>()));
          }
          injectPatternBindings(ctx, arm["pattern"], subjectPtr, subjectType);
        }

        llvm::Value *val = evaluateExpression(ctx, arm["body"]);
        if (expectedTy && val && val->getType()->isPointerTy() &&
            !expectedTy->isPointerTy()) {
          val = Builder->CreateLoad(expectedTy, val, "arm_load");
        }
        if (!val && expectedTy)
          val = llvm::Constant::getNullValue(expectedTy);

        for (const auto &key : injectedKeys)
          ctx.SymbolEnv.back().erase(key);

        llvm::BasicBlock *currentBB = Builder->GetInsertBlock();
        if (!currentBB->getTerminator()) {
          Builder->CreateBr(mergeBB);
          incomings.push_back({val, currentBB});
        }
        break;
      }
    }

    Builder->CreateCondBr(isMatch, armBB, nextBB);
    Builder->SetInsertPoint(armBB);

    std::vector<std::string> injectedKeys;
    if (arm["pattern"]["kind"] == "Variant") {
      if (arm["pattern"].contains("binding"))
        injectedKeys.push_back(
            std::string(arm["pattern"]["binding"].get<std::string_view>()));
      if (arm["pattern"].contains("fields") &&
          arm["pattern"]["fields"].is_array()) {
        for (const auto &fb : arm["pattern"]["fields"])
          injectedKeys.push_back(
              std::string(fb["binding"].get<std::string_view>()));
      }
      injectPatternBindings(ctx, arm["pattern"], subjectPtr, subjectType);
    }

    llvm::Value *val = evaluateExpression(ctx, arm["body"]);
    if (expectedTy && val && val->getType()->isPointerTy() &&
        !expectedTy->isPointerTy()) {
      val = Builder->CreateLoad(expectedTy, val, "arm_load");
    }
    if (!val && expectedTy)
      val = llvm::Constant::getNullValue(expectedTy);

    for (const auto &key : injectedKeys)
      ctx.SymbolEnv.back().erase(key);

    llvm::BasicBlock *currentBB = Builder->GetInsertBlock();
    if (!currentBB->getTerminator()) {
      Builder->CreateBr(mergeBB);
      incomings.push_back({val, currentBB});
    }
    Builder->SetInsertPoint(nextBB);
  }

  llvm::BasicBlock *fallthroughBB = Builder->GetInsertBlock();
  if (!fallthroughBB->getTerminator()) {
    Builder->CreateBr(mergeBB);
    if (expectedTy)
      incomings.push_back(
          {llvm::Constant::getNullValue(expectedTy), fallthroughBB});
  }

  Builder->SetInsertPoint(mergeBB);
  if (expectedTy) {
    llvm::PHINode *phi = Builder->CreatePHI(expectedTy, incomings.size());
    for (auto &pair : incomings) {
      if (pair.first)
        phi->addIncoming(pair.first, pair.second);
    }
    return phi;
  }
  return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
}

llvm::Value *compileBlockExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;
  ctx.pushScope();
  llvm::Value *blockResult = nullptr;
  for (const auto &s : expr["statements"]) {
    if (s.contains("node_type") &&
        s["node_type"].get<std::string_view>() == "YieldStmt") {
      blockResult = evaluateExpression(ctx, s["value"]);
    } else {
      compileStatement(ctx, s);
    }
    if (Builder->GetInsertBlock()->getTerminator())
      break;
  }
  llvm::FunctionCallee releaseFn = ctx.Module->getOrInsertFunction(
      "maml_release", llvm::FunctionType::get(
                          llvm::Type::getVoidTy(ctx.Context),
                          {llvm::PointerType::getUnqual(ctx.Context)}, false));
  if (!ctx.ScopeStack.empty())
    ctx.popScope(releaseFn);
  return blockResult;
}

llvm::Value *compileAwaitExpression(CodegenContext &ctx,
                                    const nlohmann::json &e) {
  auto &Builder = ctx.Builder;
  llvm::Value *taskVal = evaluateExpression(ctx, e["value"]);
  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  llvm::BasicBlock *resumeBB =
      llvm::BasicBlock::Create(ctx.Context, "await.resume", parentFn);
  llvm::BasicBlock *cleanupBB =
      llvm::BasicBlock::Create(ctx.Context, "await.cleanup", parentFn);
  llvm::BasicBlock *suspendBB =
      llvm::BasicBlock::Create(ctx.Context, "await.suspend", parentFn);

  llvm::Function *coroSuspendFn = llvm::Intrinsic::getDeclaration(
      ctx.Module.get(), llvm::Intrinsic::coro_suspend);
  llvm::Value *noneToken = llvm::ConstantTokenNone::get(ctx.Context);
  llvm::Value *isFinalValue =
      llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);

  llvm::Value *suspendResult = Builder->CreateCall(
      coroSuspendFn, {noneToken, isFinalValue}, "suspend_res");
  llvm::SwitchInst *sw = Builder->CreateSwitch(suspendResult, suspendBB, 2);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 0),
              resumeBB);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), 1),
              cleanupBB);

  Builder->SetInsertPoint(cleanupBB);
  llvm::Function *coroFreeFn = llvm::Intrinsic::getDeclaration(
      ctx.Module.get(), llvm::Intrinsic::coro_free);
  Builder->CreateCall(coroFreeFn, {ctx.resolveSymbol(rt::CORO_ID),
                                   ctx.resolveSymbol(rt::CORO_HDL)});
  Builder->CreateBr(suspendBB);

  Builder->SetInsertPoint(suspendBB);
  Builder->CreateRet(ctx.resolveSymbol(rt::CORO_HDL));

  Builder->SetInsertPoint(resumeBB);
  return taskVal;
}

} // namespace maml