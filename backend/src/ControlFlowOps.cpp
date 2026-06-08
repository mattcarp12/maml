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

  if (left->getType()->isPointerTy() && right->getType()->isIntegerTy()) {
    if (auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(right); cInt && cInt->isZero()) {
      right = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(left->getType()));
    }
  } else if (right->getType()->isPointerTy() && left->getType()->isIntegerTy()) {
    if (auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(left); cInt && cInt->isZero()) {
      left = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(right->getType()));
    }
  }

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

    // Calibrate container instantiations arguments dynamically
    if (funcName == rt::MAP_CREATE) {
      if (i == 0) {
        int itemSize = 4;
        if (stmt["type"].contains("value_type")) {
          const auto &valType = stmt["type"]["value_type"];
          if (valType.is_object() && valType.contains("size")) {
            itemSize = valType["size"].get<int>();
          } else {
            // Fallback: Resolve allocation footprint dynamically via LLVM DataLayout
            llvm::Type *llvmTy = llvmTypeFor(ctx, valType);
            llvm::DataLayout DL(ctx.Module.get());
            itemSize = DL.getTypeAllocSize(llvmTy);
          }
        }
        argVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), itemSize);
      } else if (i == 1) {
        bool isStr = stmt["type"].contains("key_type") && stmt["type"]["key_type"].get<std::string_view>() == "string";
        argVal = llvm::ConstantInt::get(llvm::Type::getInt8Ty(ctx.Context), isStr ? 1 : 0);
      }
    } else if (funcName == rt::VEC_CREATE) {
      if (i == 0) {
        int itemSize = 4;
        if (stmt["type"].contains("elem_type")) {
          const auto &elemType = stmt["type"]["elem_type"];
          if (elemType.is_object() && elemType.contains("size")) {
            itemSize = elemType["size"].get<int>();
          } else {
            // Fallback: Resolve allocation footprint dynamically via LLVM DataLayout
            llvm::Type *llvmTy = llvmTypeFor(ctx, elemType);
            llvm::DataLayout DL(ctx.Module.get());
            itemSize = DL.getTypeAllocSize(llvmTy);
          }
        }
        argVal = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), itemSize);
      }
    }

    if (!argVal) return;

    if (FT && i < FT->getNumParams()) {
      llvm::Type *expectedTy = FT->getParamType(i);
      llvm::Type *actualTy = argVal->getType();

      if (expectedTy != actualTy) {
        if (expectedTy->isPointerTy() && actualTy->isStructTy()) {
          // argVal = ctx.Builder->CreateExtractValue(argVal, {0}, "fat_ptr_unwrap");

          auto *structTy = llvm::dyn_cast<llvm::StructType>(actualTy);

          // Only unwrap literal string structs if they are being passed to non-collection functions
          // (e.g., passing a string to an external print function that expects a raw char pointer).
          // If we are interacting with runtime collections (maml_vec_* / maml_map_*), or handling
          // a named user struct like 'Point', we must pass a pointer to the structure itself.
          if (structTy && structTy->isLiteral() && funcName.compare(0, 9, "maml_vec_") != 0 &&
              funcName.compare(0, 9, "maml_map_") != 0) {
            argVal = ctx.Builder->CreateExtractValue(argVal, {0}, "fat_ptr_unwrap");
          } else {
            // Spill the structure to the entry block stack and pass the pointer instead
            llvm::Function *parentFn = ctx.Builder->GetInsertBlock()->getParent();
            llvm::IRBuilder<> TmpBuilder(&parentFn->getEntryBlock(), parentFn->getEntryBlock().begin());
            llvm::AllocaInst *spill = TmpBuilder.CreateAlloca(actualTy, nullptr, "arg_struct_spill");

            ctx.Builder->CreateStore(argVal, spill);
            argVal = spill;
          }

        } else if (expectedTy->isIntegerTy() && actualTy->isIntegerTy()) {
          argVal = ctx.Builder->CreateIntCast(argVal, expectedTy, true, "arg_cast");
        } else if (expectedTy->isPointerTy() && actualTy->isPointerTy()) {
          argVal = ctx.Builder->CreatePointerCast(argVal, expectedTy, "ptr_cast");
        }

        else if (expectedTy->isPointerTy() && !actualTy->isPointerTy()) {
          // Fix: Only treat 0 as a null pointer if it's explicitly NOT a boolean (i1)
          // or a standard data integer being boxed into an anyopaque.
          auto *cInt = llvm::dyn_cast<llvm::ConstantInt>(argVal);

          // You might need to adjust this depending on how you represent true 'null' in MAML,
          // but removing the isZero() trap for i1/i32 fixes the boolean and int push bugs!
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