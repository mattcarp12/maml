#include "ExprGenerator.h"
#include "TypeLowering.h"

namespace maml {

llvm::Value *compileCallExpr(CodegenContext &ctx, const nlohmann::json &expr) {
  auto &Builder = ctx.Builder;

  // 1. Resolve the Callee FIRST
  llvm::Value *callee = nullptr;
  std::string funcName;
  std::string_view functionNodeType = "unknown";

  if (expr["function"].contains("op")) {
    functionNodeType = expr["function"]["op"].get<std::string_view>();
  }

  if (functionNodeType == "ident") {
    funcName = expr["function"]["value"].get<std::string>();
    callee = ctx.Module->getFunction(funcName);  // Look up pre-declared runtime/user functions

    if (!callee) {
      callee = ctx.resolveSymbol(funcName);
      // Look up local function pointers
    }

    // Intercept SumType Variant Constructors!
    if (!callee && expr.contains("maml_type") && expr["maml_type"]["kind"] == "SumType") {
      nlohmann::json sumTypeJson = expr["maml_type"];
      int discriminant = -1;
      nlohmann::json variantDef;

      for (const auto &v : sumTypeJson["variants"]) {
        if (v["name"].get<std::string_view>() == funcName) {
          discriminant = v["discriminant"];
          variantDef = v;
          break;
        }
      }

      if (discriminant != -1) {
        // Construct the SumType inline!
        llvm::Type *sumTy = llvmTypeFor(ctx, sumTypeJson);
        llvm::AllocaInst *variantAlloca = Builder->CreateAlloca(sumTy, nullptr, "variant_" + funcName);

        llvm::Value *discrimGep =
            Builder->CreateGEP(sumTy, variantAlloca, {Builder->getInt32(0), Builder->getInt32(0)});
        Builder->CreateStore(Builder->getInt32(discriminant), discrimGep);

        if (!variantDef["fields"].empty()) {
          llvm::Value *payloadGep =
              Builder->CreateGEP(sumTy, variantAlloca, {Builder->getInt32(0), Builder->getInt32(1)});

          std::vector<llvm::Type *> payloadTypes;
          for (const auto &f : variantDef["fields"]) payloadTypes.push_back(llvmTypeFor(ctx, f["type"]));
          llvm::StructType *payloadStructTy = llvm::StructType::get(ctx.Context, payloadTypes);

          const auto &astArgs = expr["arguments"];

          // Map positional CallExpr AST arguments to Variant fields
          for (size_t k = 0; k < variantDef["fields"].size(); ++k) {
            if (k >= astArgs.size()) break;  // Safety

            // ✨ FIX 1: Evaluate the argument node directly instead of unwrapping "argument"
            llvm::Value *fieldVal = evaluateExpression(ctx, astArgs[k]);

            if (!fieldVal) return nullptr;

            llvm::Type *expectedTy = payloadTypes[k];

            // Coerce pointers if needed
            if (expectedTy->isPointerTy() && fieldVal->getType()->isPointerTy() && expectedTy != fieldVal->getType()) {
              fieldVal = Builder->CreatePointerCast(fieldVal, expectedTy);
            }

            llvm::Value *fieldGep = Builder->CreateGEP(payloadStructTy, payloadGep,
                                                       {Builder->getInt32(0), Builder->getInt32(static_cast<int>(k))});
            Builder->CreateStore(fieldVal, fieldGep);
          }
        }
        return variantAlloca;  // Return the constructed struct directly!
      }
    }
  } else {
    callee = evaluateExpression(ctx, expr["function"]);
  }

  // Failsafe
  if (!callee) {
    ctx.Error.fatal("Could not resolve function for call: " + funcName, expr);
    return nullptr;
  }

  // 2. Unpack function pointers
  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(callee)) {
    callee = Builder->CreateLoad(alloca->getAllocatedType(), alloca, "fn_ptr_load");
  }

  // 3. Extract the exact LLVM FunctionType (if it's a direct, known function)
  llvm::FunctionType *FT = nullptr;
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    FT = F->getFunctionType();
  }

  // 4. Evaluate and Coerce Arguments for standard functions
  std::vector<llvm::Value *> args;
  std::vector<llvm::Type *> argTys;
  size_t i = 0;

  for (const auto &arg : expr["arguments"]) {
    // ✨ FIX 2: Evaluate the argument node directly instead of unwrapping "argument"
    llvm::Value *argVal = evaluateExpression(ctx, arg);

    if (!argVal) return nullptr;

    if (FT && i < FT->getNumParams()) {
      llvm::Type *expectedTy = FT->getParamType(i);
      llvm::Type *actualTy = argVal->getType();

      if (expectedTy != actualTy) {
        // Unpack Fat Pointers (Strings/Slices) for C-ABI compatibility
        if (expectedTy->isPointerTy() && actualTy->isStructTy()) {
          argVal = Builder->CreateExtractValue(argVal, {0}, "fat_ptr_unwrap");
        }
        // Coerce integers
        else if (expectedTy->isIntegerTy() && actualTy->isIntegerTy()) {
          argVal = Builder->CreateIntCast(argVal, expectedTy, true, "arg_cast");
        }
        // Coerce opaque pointers
        else if (expectedTy->isPointerTy() && actualTy->isPointerTy()) {
          argVal = Builder->CreatePointerCast(argVal, expectedTy, "ptr_cast");
        }
      }
    }

    args.push_back(argVal);
    argTys.push_back(argVal->getType());
    i++;
  }

  std::string calleeName = expr["function"]["value"].get<std::string>();

  // -------------------------------------------------------------------------
  // 1. MAML MAP RUNTIME INTERCEPT
  // -------------------------------------------------------------------------
  if (calleeName == "maml_map_get" || calleeName == "maml_map_put") {
    llvm::Value *keyArg = args[1];
    llvm::Value *keyHash = nullptr;
    llvm::Value *strPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(ctx.Context));
    llvm::Value *strLen = ctx.Builder->getInt32(0);

    // A. Determine if key is a String or Primitive
    if (keyArg->getType()->isStructTy()) {
      // MAML Strings are lowered as { ptr, i32 } structs.
      // Extract the pointer and the length.
      strPtr = ctx.Builder->CreateExtractValue(keyArg, {0}, "str_ptr");
      strLen = ctx.Builder->CreateExtractValue(keyArg, {1}, "str_len");

      // Hash the string at runtime by invoking maml_str_hash
      llvm::FunctionCallee hashFn = ctx.Module->getOrInsertFunction(
          "maml_str_hash",
          llvm::FunctionType::get(ctx.Builder->getInt64Ty(),
                                  {llvm::PointerType::getUnqual(ctx.Context), ctx.Builder->getInt32Ty()}, false));
      keyHash = ctx.Builder->CreateCall(hashFn, {strPtr, strLen}, "hash_tmp");
    } else {
      // Primitive Integer Keys -> Zero-extend to u64 for the key_hash
      if (keyArg->getType()->getIntegerBitWidth() < 64) {
        keyHash = ctx.Builder->CreateZExt(keyArg, ctx.Builder->getInt64Ty(), "key_hash_zext");
      } else {
        keyHash = keyArg;  // Already 64-bit
      }
    }

    // B. Rebuild the LLVM arguments array to match the strict Zig signatures
    if (calleeName == "maml_map_get") {
      args = {args[0], keyHash, strPtr, strLen};
    } else if (calleeName == "maml_map_put") {
      llvm::Value *valArg = args[2];

      // Zig expects a pointer to the value bytes so it can memcpy them
      if (!valArg->getType()->isPointerTy()) {
        llvm::AllocaInst *valSpill = ctx.Builder->CreateAlloca(valArg->getType(), nullptr, "map_val_spill");
        ctx.Builder->CreateStore(valArg, valSpill);
        valArg = valSpill;
      }

      args = {args[0], keyHash, valArg, strPtr, strLen};
    }
  }

  llvm::CallInst *callResult = ctx.Builder->CreateCall(FT, callee, args, "calltmp");

  // -------------------------------------------------------------------------
  // 2. MAML MAP RUNTIME INTERCEPT: Fix Return Values
  // -------------------------------------------------------------------------
  if (calleeName == "maml_map_get") {
    // The Zig runtime returns `?*anyopaque` (an opaque pointer to the map slot).
    // LLVM arithmetic operators need the actual integer, so we must emit a LoadInst.

    // Attempt to extract the expected type from the MIR (e.g., i32).
    // If your MIR 'call' node doesn't explicitly attach the return type,
    // you can temporarily fallback to getInt32Ty while testing.
    llvm::Type *valTy;
    if (expr.contains("type")) {
      valTy = llvmTypeFor(ctx, expr["type"]);
    } else {
      valTy = llvm::Type::getInt32Ty(ctx.Context);  // Fallback for integer testing
    }

    return ctx.Builder->CreateLoad(valTy, callResult, "map_val_load");
  }

  return callResult;
}

}  // namespace maml