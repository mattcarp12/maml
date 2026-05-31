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

  // 5. Fallback FunctionType generation for indirect/opaque function pointers
  if (!FT) {
    llvm::Type *retTy = llvmTypeFor(ctx, expr["maml_type"]);
    FT = llvm::FunctionType::get(retTy, argTys, false);
  }

  // 6. Emit the Call
  return Builder->CreateCall(FT, callee, args, "calltmp");
}

}  // namespace maml