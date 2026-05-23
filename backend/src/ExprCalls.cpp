#include "ExprGenerator.h"
#include "MemoryManager.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *handleVectorBuiltin(CodegenContext &ctx,
                                 const nlohmann::json &expr,
                                 llvm::Value *objVal, llvm::Type *objTy,
                                 std::string_view methodName) {
  auto &Builder = ctx.Builder;
  llvm::Value *vecPtr = nullptr;

  if (expr["function"]["object"].contains("node_type") &&
      expr["function"]["object"]["node_type"].get<std::string_view>() ==
          "Identifier") {
    vecPtr = ctx.resolveSymbol(
        expr["function"]["object"]["value"].get<std::string_view>());
  } else {
    if (!objVal->getType()->isPointerTy()) {
      llvm::AllocaInst *spill =
          Builder->CreateAlloca(objTy, nullptr, "vec_spill");
      Builder->CreateStore(objVal, spill);
      vecPtr = spill;
    } else {
      vecPtr = objVal;
    }
  }

  if (methodName == "len") {
    llvm::Value *lenGep = Builder->CreateGEP(
        objTy, vecPtr, {Builder->getInt32(0), Builder->getInt32(2)},
        "vec_len_ptr");
    return Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep,
                               "vec_len");
  }

  if (methodName == "push") {
    llvm::Function *F = Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock *growBB =
        llvm::BasicBlock::Create(ctx.Context, "vec_grow", F);
    llvm::BasicBlock *insertBB =
        llvm::BasicBlock::Create(ctx.Context, "vec_insert", F);

    llvm::Value *rawPtrGep = Builder->CreateGEP(
        objTy, vecPtr, {Builder->getInt32(0), Builder->getInt32(0)});
    llvm::Value *dataPtrGep = Builder->CreateGEP(
        objTy, vecPtr, {Builder->getInt32(0), Builder->getInt32(1)});
    llvm::Value *lenGep = Builder->CreateGEP(
        objTy, vecPtr, {Builder->getInt32(0), Builder->getInt32(2)});
    llvm::Value *capGep = Builder->CreateGEP(
        objTy, vecPtr, {Builder->getInt32(0), Builder->getInt32(3)});

    llvm::Value *currentLen =
        Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), lenGep);
    llvm::Value *currentCap =
        Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), capGep);

    llvm::Value *isFull =
        Builder->CreateICmpEQ(currentLen, currentCap, "is_full");
    Builder->CreateCondBr(isFull, growBB, insertBB);

    Builder->SetInsertPoint(growBB);
    llvm::Value *currentRawPtr = Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context), rawPtrGep, "cur_raw_ptr");

    llvm::Type *baseTy =
        llvmTypeFor(ctx, expr["function"]["object"]["maml_type"]["base"]);
    llvm::DataLayout DL(ctx.Module.get());
    llvm::Value *itemSize = Builder->getInt32(DL.getTypeAllocSize(baseTy));

    llvm::FunctionCallee vecGrowFn = ctx.Module->getOrInsertFunction(
        rt::VEC_GROW,
        llvm::FunctionType::get(llvm::PointerType::getUnqual(ctx.Context),
                                {llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt32Ty(ctx.Context),
                                 llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt32Ty(ctx.Context)},
                                false));

    // maml_vec_grow(rawPtr, currentLen, &cap, itemSize) -> new raw allocation.
    // It updates the capacity field in-place via the capGep pointer.
    llvm::Value *newRawPtr = Builder->CreateCall(
        vecGrowFn, {currentRawPtr, currentLen, capGep, itemSize},
        "new_raw_ptr");

    // rawPtr (field 0) = the new allocation root, used by ARC retain/release.
    Builder->CreateStore(newRawPtr, rawPtrGep);

    // dataPtr (field 1) = pointer to element zero of the new allocation.
    // Derived via GEP so the separation between allocation root and data start
    // is explicit and survives any future runtime header changes.
    llvm::Value *newDataPtr = Builder->CreateGEP(
        baseTy, newRawPtr, Builder->getInt32(0), "new_data_ptr");
    Builder->CreateStore(newDataPtr, dataPtrGep);

    Builder->CreateBr(insertBB);

    Builder->SetInsertPoint(insertBB);
    // Reload freshly — either the grow path updated them, or the no-grow path
    // left the originals in place. Either way, load from the struct fields.
    llvm::Value *freshDataPtr =
        Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context),
                            dataPtrGep, "fresh_data_ptr");
    llvm::Value *freshLen = Builder->CreateLoad(
        llvm::Type::getInt32Ty(ctx.Context), lenGep, "fresh_len");
    llvm::Value *itemVal =
        evaluateExpression(ctx, expr["arguments"][0]["value"]);

    llvm::Value *targetElemPtr =
        Builder->CreateGEP(baseTy, freshDataPtr, freshLen, "target_elem_ptr");
    Builder->CreateStore(itemVal, targetElemPtr);

    llvm::Value *newLen = Builder->CreateAdd(freshLen, Builder->getInt32(1));
    Builder->CreateStore(newLen, lenGep);

    return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
  }
  return nullptr;
}

llvm::Value *handleMapBuiltin(CodegenContext &ctx, const nlohmann::json &expr,
                              llvm::Value *objVal, llvm::Type *objTy,
                              std::string_view methodName) {
  auto &Builder = ctx.Builder;
  llvm::Value *mapPtr = nullptr;
  if (expr["function"]["object"].contains("node_type") &&
      expr["function"]["object"]["node_type"].get<std::string_view>() ==
          "Identifier") {
    mapPtr = ctx.resolveSymbol(
        expr["function"]["object"]["value"].get<std::string_view>());
  } else {
    llvm::AllocaInst *spill =
        Builder->CreateAlloca(objTy, nullptr, "map_val_spill");
    Builder->CreateStore(objVal, spill);
    mapPtr = spill;
  }

  mapPtr = Builder->CreatePointerCast(
      mapPtr, llvm::PointerType::getUnqual(ctx.Context));
  llvm::Value *keyVal = evaluateExpression(ctx, expr["arguments"][0]["value"]);

  llvm::Value *keyHash = nullptr;
  llvm::Value *strPtr =
      llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));
  llvm::Value *strLen = Builder->getInt32(0);

  if (expr["arguments"][0]["value"]["maml_type"]["kind"]
          .get<std::string_view>() == "String") {
    llvm::Type *strTy =
        llvmTypeFor(ctx, expr["arguments"][0]["value"]["maml_type"]);
    llvm::Value *strPtrGep = Builder->CreateGEP(
        strTy, keyVal, {Builder->getInt32(0), Builder->getInt32(0)});
    llvm::Value *strLenGep = Builder->CreateGEP(
        strTy, keyVal, {Builder->getInt32(0), Builder->getInt32(1)});
    strPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context),
                                 strPtrGep);
    strLen =
        Builder->CreateLoad(llvm::Type::getInt32Ty(ctx.Context), strLenGep);

    llvm::FunctionCallee strHashFn = ctx.Module->getOrInsertFunction(
        rt::STR_HASH,
        llvm::FunctionType::get(llvm::Type::getInt64Ty(ctx.Context),
                                {llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt32Ty(ctx.Context)},
                                false));
    keyHash = Builder->CreateCall(strHashFn, {strPtr, strLen});
  } else {
    keyHash = Builder->CreateZExt(keyVal, llvm::Type::getInt64Ty(ctx.Context));
  }

  if (methodName == "put") {
    llvm::Value *valVal =
        evaluateExpression(ctx, expr["arguments"][1]["value"]);
    llvm::Type *valTy =
        llvmTypeFor(ctx, expr["arguments"][1]["value"]["maml_type"]);

    llvm::AllocaInst *valAlloca =
        Builder->CreateAlloca(valTy, nullptr, "map_val_tmp");
    Builder->CreateStore(valVal, valAlloca);
    llvm::Value *valRawPtr = Builder->CreatePointerCast(
        valAlloca, llvm::PointerType::getUnqual(ctx.Context));

    llvm::FunctionCallee mapPutFn = ctx.Module->getOrInsertFunction(
        rt::MAP_PUT,
        llvm::FunctionType::get(llvm::Type::getVoidTy(ctx.Context),
                                {llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt64Ty(ctx.Context),
                                 llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt32Ty(ctx.Context)},
                                false));
    Builder->CreateCall(mapPutFn, {mapPtr, keyHash, valRawPtr, strPtr, strLen});
    return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
  }

  if (methodName == "get") {
    llvm::FunctionCallee mapGetFn = ctx.Module->getOrInsertFunction(
        rt::MAP_GET,
        llvm::FunctionType::get(llvm::PointerType::getUnqual(ctx.Context),
                                {llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt64Ty(ctx.Context),
                                 llvm::PointerType::getUnqual(ctx.Context),
                                 llvm::Type::getInt32Ty(ctx.Context)},
                                false));

    llvm::Value *rawValPtr =
        Builder->CreateCall(mapGetFn, {mapPtr, keyHash, strPtr, strLen});
    llvm::Type *retTy = llvmTypeFor(ctx, expr["maml_type"]);
    return Builder->CreateLoad(retTy, rawValPtr, "map_val_load");
  }
  return nullptr;
}

llvm::Value *compileCallExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;

  std::string_view functionNodeType = "unknown";
  if (expr["function"].contains("node_type")) {
    functionNodeType = expr["function"]["node_type"].get<std::string_view>();
  }

  if (functionNodeType == "FieldAccess") {
    llvm::Value *objVal = evaluateExpression(ctx, expr["function"]["object"]);
    std::string_view methodName =
        expr["function"]["field"].get<std::string_view>();
    std::string_view objKind =
        expr["function"]["object"]["maml_type"]["kind"].get<std::string_view>();
    llvm::Type *objTy =
        llvmTypeFor(ctx, expr["function"]["object"]["maml_type"]);

    if (objKind == "Vector")
      return handleVectorBuiltin(ctx, expr, objVal, objTy, methodName);
    if (objKind == "Map")
      return handleMapBuiltin(ctx, expr, objVal, objTy, methodName);
  }

  if (functionNodeType == "Identifier") {
    std::string_view funcName =
        expr["function"]["value"].get<std::string_view>();

    if (expr.contains("maml_type") && expr["maml_type"].contains("kind") &&
        expr["maml_type"]["kind"].get<std::string_view>() == "SumType") {
      nlohmann::json sumTy = expr["maml_type"];
      for (const auto &variantDef : sumTy["variants"]) {
        if (variantDef["name"].get<std::string_view>() == funcName) {
          nlohmann::json variantNode;
          variantNode["node_type"] = "VariantLiteral";
          variantNode["variant_name"] = funcName;
          variantNode["maml_type"] = sumTy;
          variantNode["fields"] = nlohmann::json::array();
          if (expr.contains("arguments") && expr["arguments"].is_array()) {
            int i = 0;
            for (const auto &arg : expr["arguments"]) {
              if (i < variantDef["fields"].size()) {
                nlohmann::json fieldObj;
                fieldObj["name"] = variantDef["fields"][i]["name"];
                fieldObj["value"] = arg["value"];
                variantNode["fields"].push_back(fieldObj);
              }
              i++;
            }
          }
          return evaluateExpression(ctx, variantNode);
        }
      }
    }

    if (funcName == "spawn") {
      llvm::Value *taskArg =
          evaluateExpression(ctx, expr["arguments"][0]["value"]);
      llvm::FunctionCallee spawnFn = ctx.Module->getOrInsertFunction(
          rt::SPAWN_TASK, llvm::Type::getVoidTy(ctx.Context),
          llvm::PointerType::getUnqual(ctx.Context));
      Builder->CreateCall(spawnFn, {taskArg});
      return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
    }
    if (funcName == "run_executor") {
      llvm::FunctionCallee runFn = ctx.Module->getOrInsertFunction(
          rt::RUN_EXECUTOR, llvm::Type::getVoidTy(ctx.Context));
      Builder->CreateCall(runFn, {});
      return llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0);
    }
    if (funcName == "puts") {
      llvm::Value *argVal =
          evaluateExpression(ctx, expr["arguments"][0]["value"]);
      std::string_view argKind =
          expr["arguments"][0]["value"]["maml_type"]["kind"]
              .get<std::string_view>();
      llvm::Value *cStringPtr = argVal;

      if (argKind == "String") {
        llvm::Type *stringTy =
            llvmTypeFor(ctx, expr["arguments"][0]["value"]["maml_type"]);
        llvm::Value *stringPtr = argVal;
        if (!argVal->getType()->isPointerTy()) {
          llvm::AllocaInst *spill =
              Builder->CreateAlloca(stringTy, nullptr, "puts_spill");
          Builder->CreateStore(argVal, spill);
          stringPtr = spill;
        }
        llvm::Value *ptrGep = Builder->CreateGEP(
            stringTy, stringPtr, {Builder->getInt32(0), Builder->getInt32(0)},
            "get_char_ptr");
        cStringPtr = Builder->CreateLoad(
            llvm::PointerType::getUnqual(ctx.Context), ptrGep, "raw_c_str");
      }
      llvm::FunctionCallee putsFn = ctx.Module->getOrInsertFunction(
          rt::PUTS, llvm::FunctionType::get(
                        llvm::Type::getInt32Ty(ctx.Context),
                        {llvm::PointerType::getUnqual(ctx.Context)}, false));
      return Builder->CreateCall(putsFn, {cStringPtr});
    }

    llvm::StringRef llFuncName(funcName.data(), funcName.size());
    llvm::Function *calleeFn = ctx.Module->getFunction(llFuncName);
    if (!calleeFn) {
      std::vector<llvm::Type *> argTypes;
      for (const auto &arg : expr["arguments"])
        argTypes.push_back(llvmTypeFor(ctx, arg["value"]["maml_type"]));
      llvm::Type *retType = llvmTypeFor(ctx, expr["maml_type"]);
      llvm::FunctionType *FT =
          llvm::FunctionType::get(retType, argTypes, false);
      calleeFn = llvm::Function::Create(FT, llvm::Function::ExternalLinkage,
                                        llFuncName, *ctx.Module);
    }

    std::vector<llvm::Value *> argsV;
    int argIdx = 0;
    for (const auto &arg : expr["arguments"]) {
      llvm::Value *val = evaluateExpression(ctx, arg["value"]);
      if (calleeFn && argIdx < calleeFn->getFunctionType()->getNumParams()) {
        llvm::Type *expectedArgTy =
            calleeFn->getFunctionType()->getParamType(argIdx);
        if (val->getType()->isPointerTy() && !expectedArgTy->isPointerTy()) {
          val = Builder->CreateLoad(expectedArgTy, val, "arg_load");
        }
      }
      if (arg.value("is_own", false)) {
        if (arg["value"].contains("node_type") &&
            arg["value"]["node_type"].get<std::string_view>() == "Identifier") {
          std::string_view varName =
              arg["value"]["value"].get<std::string_view>();
          if (llvm::Value *ptr = ctx.resolveSymbol(varName)) {
            MemoryManager::untrackFromRelease(ctx, ptr);
          }
        }
      }
      argsV.push_back(val);
      argIdx++;
    }
    return Builder->CreateCall(calleeFn, argsV, "calltmp");
  }
  return nullptr;
}

} // namespace maml