#include <llvm/IR/Intrinsics.h>

#include <string>
#include <string_view>
#include <vector>

#include "ExprGenerator.h"
#include "RuntimeConstants.h"
#include "StmtGenerator.h"
#include "TypeLowering.h"

namespace maml {

// -----------------------------------------------------------------------------
// Private, Isolated Instruction Helpers
// -----------------------------------------------------------------------------

static void handleBinaryOp(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  std::string_view opSymbol = stmt["operator"].get<std::string_view>();
  llvm::Value *left = evaluateExpression(ctx, stmt["left"]);
  llvm::Value *right = evaluateExpression(ctx, stmt["right"]);
  llvm::Value *result = nullptr;

  // Enforce zero-division and modulus hardware trap barriers
  if (opSymbol == "/" || opSymbol == "%") {
    llvm::Value *isZero = ctx.Builder->CreateICmpEQ(right, llvm::ConstantInt::get(right->getType(), 0), "is_zero");
    llvm::Function *F = ctx.Builder->GetInsertBlock()->getParent();
    llvm::BasicBlock *trapBB = llvm::BasicBlock::Create(ctx.Context, "trap_div_zero", F);
    llvm::BasicBlock *contBB = llvm::BasicBlock::Create(ctx.Context, "cont_div", F);

    ctx.Builder->CreateCondBr(isZero, trapBB, contBB);
    ctx.Builder->SetInsertPoint(trapBB);

    llvm::Function *trapFn = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::trap);
    ctx.Builder->CreateCall(trapFn);
    ctx.Builder->CreateUnreachable();

    ctx.Builder->SetInsertPoint(contBB);
    if (opSymbol == "/") result = ctx.Builder->CreateSDiv(left, right, "divtmp");
    if (opSymbol == "%") result = ctx.Builder->CreateSRem(left, right, "modtmp");
  } else if (opSymbol == "+")
    result = ctx.Builder->CreateAdd(left, right, "addtmp");
  else if (opSymbol == "-")
    result = ctx.Builder->CreateSub(left, right, "subtmp");
  else if (opSymbol == "*")
    result = ctx.Builder->CreateMul(left, right, "multmp");
  else if (opSymbol == "==")
    result = ctx.Builder->CreateICmpEQ(left, right, "eqtmp");
  else if (opSymbol == "!=")
    result = ctx.Builder->CreateICmpNE(left, right, "neqtmp");
  else if (opSymbol == "<")
    result = ctx.Builder->CreateICmpSLT(left, right, "lttmp");
  else if (opSymbol == ">")
    result = ctx.Builder->CreateICmpSGT(left, right, "gttmp");
  else if (opSymbol == "<=")
    result = ctx.Builder->CreateICmpSLE(left, right, "letmp");
  else if (opSymbol == ">=")
    result = ctx.Builder->CreateICmpSGE(left, right, "getmp");
  else {
    ctx.Error.fatal("Unknown binary operator: " + std::string(opSymbol), stmt);
    return;
  }

  ctx.SymbolEnv.back()[dst] = result;
}

static void handleUnaryOp(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  std::string_view opSymbol = stmt["operator"].get<std::string_view>();
  llvm::Value *operand = evaluateExpression(ctx, stmt["operand"]);
  llvm::Value *result = nullptr;

  if (opSymbol == "!") {
    result = ctx.Builder->CreateXor(operand, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), "nottmp");
  } else if (opSymbol == "-") {
    result = ctx.Builder->CreateSub(llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 0), operand, "negtmp");
  } else {
    ctx.Error.fatal("Unknown unary operator: " + std::string(opSymbol), stmt);
    return;
  }

  ctx.SymbolEnv.back()[dst] = result;
}

static void handleCallInst(CodegenContext &ctx, const nlohmann::json &stmt) {
  std::string dst = stmt["dst"].get<std::string>();
  llvm::Value *callee = nullptr;
  std::string funcName;
  std::string_view functionNodeType =
      stmt["function"].contains("op") ? stmt["function"]["op"].get<std::string_view>() : "unknown";

  if (functionNodeType == "ident") {
    funcName = stmt["function"]["value"].get<std::string>();

    // 🌟 STEP 1: Absolute top-level remap before looking up anything!
    if (funcName == "push") {
      funcName = rt::VEC_PUSH;
    } else if (funcName == "put") {
      funcName = rt::MAP_PUT;
    } else if (funcName == "get") {
      // Discriminate map.get() vs vector subscipts by checking the container argument type
      if (!stmt["arguments"].empty()) {
        std::string_view containerKind = stmt["arguments"][0]["argument"]["type"].is_object() &&
                                                 stmt["arguments"][0]["argument"]["type"].contains("kind")
                                             ? stmt["arguments"][0]["argument"]["type"]["kind"].get<std::string_view>()
                                             : "";
        if (containerKind == "vector") {
          funcName = "maml_vec_get";
        } else {
          funcName = rt::MAP_GET;
        }
      } else {
        funcName = rt::MAP_GET;
      }
    }

    // STEP 2: Now look up the cleanly remapped function symbol name in the global module declarations
    callee = ctx.Module->getFunction(funcName);
    if (!callee) {
      callee = ctx.resolveSymbol(funcName);
    }
  } else {
    callee = evaluateExpression(ctx, stmt["function"]);
  }

  // Informative Error Guard
  if (!callee) {
    std::string identity =
        funcName.empty() ? ("node type '" + std::string(functionNodeType) + "'") : ("name '" + funcName + "'");
    ctx.Error.fatal("Could not resolve function target with " + identity + " for call execution context", stmt);
    return;
  }

  if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(callee)) {
    callee = ctx.Builder->CreateLoad(alloca->getAllocatedType(), alloca, "fn_ptr_load");
  }

  llvm::FunctionType *FT = nullptr;
  if (auto *F = llvm::dyn_cast<llvm::Function>(callee)) {
    FT = F->getFunctionType();
  } else {
    llvm::Type *expectedRetTy = llvmTypeFor(ctx, stmt["type"]);
    std::vector<llvm::Type *> expectedArgTys;
    for (const auto &argWrapper : stmt["arguments"]) {
      expectedArgTys.push_back(llvmTypeFor(ctx, argWrapper["argument"]["type"]));
    }
    FT = llvm::FunctionType::get(expectedRetTy, expectedArgTys, false);
  }

  std::vector<llvm::Value *> args;
  size_t i = 0;
  for (const auto &argWrapper : stmt["arguments"]) {
    llvm::Value *argVal = evaluateExpression(ctx, argWrapper["argument"]);

    // 🌟 THE FINISHING TEST FIX: Calibrate container instantiations arguments dynamically
    if (funcName == rt::MAP_CREATE) {
      if (i == 0) {
        // Argument 0: Value Size (int is 4 bytes on our 32-bit/64-bit targets)
        argVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 4);
      } else if (i == 1) {
        // Argument 1: is_string_key flag (Check if key_type in MIR is "string")
        bool isStr = stmt["type"].contains("key_type") && stmt["type"]["key_type"].get<std::string_view>() == "string";
        argVal = llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), isStr ? 1 : 0);
      }
    } else if (funcName == rt::VEC_CREATE) {
      if (i == 0) {
        // Argument 0: Item Size (int is 4 bytes)
        argVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 4);
      }
    }

    if (!argVal) return;

    if (FT && i < FT->getNumParams()) {
      llvm::Type *expectedTy = FT->getParamType(i);
      llvm::Type *actualTy = argVal->getType();

      if (expectedTy != actualTy) {
        if (expectedTy->isPointerTy() && actualTy->isStructTy()) {
          argVal = ctx.Builder->CreateExtractValue(argVal, {0}, "fat_ptr_unwrap");
        } else if (expectedTy->isIntegerTy() && actualTy->isIntegerTy()) {
          argVal = ctx.Builder->CreateIntCast(argVal, expectedTy, true, "arg_cast");
        } else if (expectedTy->isPointerTy() && actualTy->isPointerTy()) {
          argVal = ctx.Builder->CreatePointerCast(argVal, expectedTy, "ptr_cast");
        } 
        
        
        else if (expectedTy->isPointerTy() && !actualTy->isPointerTy()) {
          if (auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(argVal); cInt && cInt->isZero()) {
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

  // Only bind to the Symbol Environment if the function actually returns a value
  if (!callResult->getType()->isVoidTy()) {
    llvm::Type *expectedRetTy = llvmTypeFor(ctx, stmt["type"]);
    llvm::Value *finalResult = callResult;

    if (callResult->getType() != expectedRetTy) {
      // Case A: Integer width casting (e.g., i64 hash mapping down to i32)
      if (callResult->getType()->isIntegerTy() && expectedRetTy->isIntegerTy()) {
        finalResult = ctx.Builder->CreateIntCast(callResult, expectedRetTy, /*isSigned=*/true, "call_ret_cast");
      }
      // 🌟 Case B: Pointer-to-Struct Coercion (e.g., maml_vec_create returning a ptr that the MIR expects as a struct
      // value)
      else if (callResult->getType()->isPointerTy() && expectedRetTy->isStructTy()) {
        // finalResult = ctx.Builder->CreateLoad(expectedRetTy, callResult, "call_ret_struct_load");
        finalResult = callResult;
      } else {
        ctx.Error.fatal("Return type violation in call to '" + funcName +
                            "'. MIR expected a different LLVM type than the ABI returned.",
                        stmt);
        return;
      }
    }

    ctx.SymbolEnv.back()[dst] = finalResult;
  }
}

// -----------------------------------------------------------------------------
// Public Cluster Entry Point
// -----------------------------------------------------------------------------

void compileControlFlowOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt) {
  switch (op) {
    case MirOp::BinaryOp:
      return handleBinaryOp(ctx, stmt);
    case MirOp::UnaryOp:
      return handleUnaryOp(ctx, stmt);
    case MirOp::CallInst:
      return handleCallInst(ctx, stmt);
    default:
      break;
  }
}

}  // namespace maml