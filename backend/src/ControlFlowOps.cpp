#include <cstddef>
#include <llvm/IR/Intrinsics.h>
#include <string>
#include <variant>
#include <vector>

#include "CodegenContext.hpp"
#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"
#include "mir.h"
#include "token.h"
#include "llvm/IR/BasicBlock.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/DerivedTypes.h"
#include "llvm/IR/Instructions.h"
#include "llvm/IR/Value.h"
#include "llvm/Support/Casting.h"

namespace maml {

static std::string tokenText(TokenType t)
{
    switch (t) {
    case TokenType::PLUS:
        return "+";
    case TokenType::MINUS:
        return "-";
    case TokenType::MULTIPLY:
        return "*";
    case TokenType::DIVIDE:
        return "/";
    case TokenType::MODULO:
        return "%";
    case TokenType::EQ:
        return "==";
    case TokenType::NOT_EQ:
        return "!=";
    case TokenType::LT:
        return "<";
    case TokenType::GT:
        return ">";
    case TokenType::LTE:
        return "<=";
    case TokenType::GTE:
        return ">=";
    case TokenType::NOT:
        return "!";
    default:
        return "?";
    }
}

void handle(CodegenContext& ctx, const mir::BinaryOpInst& inst)
{
    ctx.CurrentInstructionName = "BinaryOpInst (" + tokenText(inst.op) + ")";

    llvm::Value* left = evaluateValue(ctx, inst.left);
    llvm::Value* right = evaluateValue(ctx, inst.right);

    if (!left || !right) {
        ctx.Error.fatal(
            "Null operand encountered in BinaryOpInst near token: " + tokenText(inst.op));
        return;
    }

    // Handle Pointer vs Integer null comparison
    if (left->getType()->isPointerTy() && right->getType()->isIntegerTy()) {
        if (auto* cInt = llvm::dyn_cast<llvm::ConstantInt>(right); cInt && cInt->isZero())
            right = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(left->getType()));
    } else if (right->getType()->isPointerTy() && left->getType()->isIntegerTy()) {
        if (auto* cInt = llvm::dyn_cast<llvm::ConstantInt>(left); cInt && cInt->isZero())
            left = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(right->getType()));
    }

    // Coerce integer widths if they mismatch
    if (left->getType()->isIntegerTy() && right->getType()->isIntegerTy()) {
        if (left->getType() != right->getType()) {
            right = ctx.Builder->CreateSExtOrTrunc(right, left->getType(), "binop_cast");
        }
    }

    llvm::Value* result = nullptr;
    auto opType = static_cast<TokenType>(inst.op);

    switch (opType) {
    case TokenType::PLUS:
    case TokenType::PLUS_EQ:
        result = ctx.Builder->CreateAdd(left, right, "addtmp");
        break;

    case TokenType::MINUS:
    case TokenType::MINUS_EQ:
        result = ctx.Builder->CreateSub(left, right, "subtmp");
        break;

    case TokenType::MULTIPLY:
    case TokenType::MUL_EQ:
        result = ctx.Builder->CreateMul(left, right, "multmp");
        break;

    case TokenType::DIVIDE:
    case TokenType::DIV_EQ:
    case TokenType::MODULO: {
        llvm::Value* isZero
            = ctx.Builder->CreateICmpEQ(right, llvm::ConstantInt::get(right->getType(), 0));
        llvm::Function* F = ctx.Builder->GetInsertBlock()->getParent();
        llvm::BasicBlock* trapBB = llvm::BasicBlock::Create(ctx.Context, "trap_div_zero", F);
        llvm::BasicBlock* contBB = llvm::BasicBlock::Create(ctx.Context, "cont_div", F);

        ctx.Builder->CreateCondBr(isZero, trapBB, contBB);
        ctx.Builder->SetInsertPoint(trapBB);
        llvm::Function* trapFn
            = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::trap);
        ctx.Builder->CreateCall(trapFn);
        ctx.Builder->CreateUnreachable();

        ctx.Builder->SetInsertPoint(contBB);
        if (opType == TokenType::DIVIDE || opType == TokenType::DIV_EQ) {
            result = ctx.Builder->CreateSDiv(left, right, "divtmp");
        } else {
            result = ctx.Builder->CreateSRem(left, right, "modtmp");
        }
        break;
    }

    case TokenType::EQ:
        result = ctx.Builder->CreateICmpEQ(left, right, "eqtmp");
        break;

    case TokenType::NOT_EQ:
        result = ctx.Builder->CreateICmpNE(left, right, "neqtmp");
        break;

    case TokenType::LT:
        result = ctx.Builder->CreateICmpSLT(left, right, "lttmp");
        break;

    case TokenType::GT:
        result = ctx.Builder->CreateICmpSGT(left, right, "gttmp");
        break;

    case TokenType::LTE:
        result = ctx.Builder->CreateICmpSLE(left, right, "letmp");
        break;

    case TokenType::GTE:
        result = ctx.Builder->CreateICmpSGE(left, right, "getmp");
        break;

    case TokenType::AND:
        result = ctx.Builder->CreateAnd(left, right, "andtmp");
        break;

    case TokenType::OR:
        result = ctx.Builder->CreateOr(left, right, "ortmp");
        break;

    default:
        ctx.Error.fatal("Unhandled binary operator token type: " + tokenText(inst.op)
            + " (ID: " + std::to_string(static_cast<int>(opType)) + ")");
        return;
    }

    if (!result) {
        ctx.Error.fatal("BinaryOpInst evaluated to null LLVM Value");
        return;
    }

    if (llvm::Value* existing = ctx.resolveSymbol(inst.dst)) {
        if (llvm::isa<llvm::AllocaInst>(existing)) {
            ctx.Builder->CreateStore(result, existing);
        } else {
            ctx.SymbolEnv.back()[inst.dst] = result;
        }
    } else {
        ctx.SymbolEnv.back()[inst.dst] = result;
    }
}

void handle(CodegenContext& ctx, const mir::UnaryOpInst& inst)
{
    ctx.CurrentInstructionName = "UnaryOpInst (" + tokenText(inst.op) + ")";

    llvm::Value* operand = evaluateValue(ctx, inst.operand);
    llvm::Value* result = nullptr;

    std::string op = tokenText(inst.op);
    if (op == "!") {
        result = ctx.Builder->CreateXor(
            operand, llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 1), "nottmp");
    } else if (op == "-") {
        result = ctx.Builder->CreateSub(
            llvm::ConstantInt::get(operand->getType(), 0), operand, "negtmp");
    }
    ctx.SymbolEnv.back()[inst.dst] = result;
}

static std::vector<llvm::Value*> prepareCallArguments(CodegenContext& ctx,
    const mir::CallInst& inst, llvm::FunctionType* FT, const std::string& funcName)
{
    std::vector<llvm::Value*> args;
    size_t i = 0;

    for (const auto& argWrapper : inst.arguments) {
        llvm::Value* argVal = evaluateValue(ctx, argWrapper);

        if (FT && i < FT->getNumParams()) {
            llvm::Type* expectedTy = FT->getParamType(i);
            llvm::Type* actualTy = argVal->getType();

            if (expectedTy != actualTy) {
                // We only retain simple LLVM-level type coercions (like integer resizing or
                // bitcasts). Structural memory logic has been entirely offloaded to the MIR.
                if (expectedTy->isIntegerTy() && actualTy->isIntegerTy()) {
                    argVal = ctx.Builder->CreateIntCast(argVal, expectedTy, false, "arg_cast");
                } else if (expectedTy->isPointerTy() && actualTy->isPointerTy()) {
                    argVal = ctx.Builder->CreatePointerCast(argVal, expectedTy, "ptr_cast");
                } else if (expectedTy->isPointerTy() && !actualTy->isPointerTy()) {
                    auto* cInt = llvm::dyn_cast<llvm::ConstantInt>(argVal);
                    if (cInt && cInt->isZero() && actualTy->isIntegerTy(64)) {
                        argVal = llvm::ConstantPointerNull::get(
                            llvm::cast<llvm::PointerType>(expectedTy));
                    } else {
                        // Strict Observability Check: We should never hit this anymore!
                        std::string errMsg = "Type mismatch for argument " + std::to_string(i)
                            + " in " + funcName + ".\n"
                            + "     Expected: " + maml::ErrorHandler::stringify(expectedTy) + "\n"
                            + "     Got:      " + maml::ErrorHandler::stringify(actualTy) + "\n"
                            + "     Value:    " + maml::ErrorHandler::stringify(argVal);
                        ctx.Error.fatal(errMsg);
                    }
                } else {
                    std::string errMsg = "Type mismatch for argument " + std::to_string(i) + " in "
                        + funcName + ".\n"
                        + "     Expected: " + maml::ErrorHandler::stringify(expectedTy) + "\n"
                        + "     Got:      " + maml::ErrorHandler::stringify(actualTy) + "\n"
                        + "     Value:    " + maml::ErrorHandler::stringify(argVal);
                    ctx.Error.fatal(errMsg);
                }
            }
        }
        args.push_back(argVal);
        i++;
    }
    return args;
}

void handle(CodegenContext& ctx, const mir::CallInst& inst)
{
    llvm::Value* callee = evaluateValue(ctx, inst.function);

    std::string funcName = "";
    if (auto* F = llvm::dyn_cast<llvm::Function>(callee)) {
        funcName = F->getName().str();
    } else if (auto* reg = std::get_if<mir::Register>(&inst.function)) {
        funcName = std::string(ctx.Sym.resolve(reg->name));
    }
    ctx.CurrentInstructionName = "CallInst (" + funcName + ")";

    llvm::FunctionType* FT = nullptr;
    if (auto* F = llvm::dyn_cast<llvm::Function>(callee)) {
        FT = F->getFunctionType();
    } else {
        llvm::Type* expectedRetTy = llvmTypeFor(ctx, inst.type);
        std::vector<llvm::Type*> expectedArgTys;
        for (const auto& argWrapper : inst.arguments) {
            expectedArgTys.push_back(evaluateValue(ctx, argWrapper)->getType());
        }
        FT = llvm::FunctionType::get(expectedRetTy, expectedArgTys, false);
    }

    std::vector<llvm::Value*> args = prepareCallArguments(ctx, inst, FT, funcName);

    llvm::CallInst* callResult = FT && FT->getReturnType()->isVoidTy()
        ? ctx.Builder->CreateCall(FT, callee, args)
        : ctx.Builder->CreateCall(FT, callee, args, "calltmp");

    if (!callResult->getType()->isVoidTy()) {
        llvm::Type* expectedRetTy = llvmTypeFor(ctx, inst.type);
        llvm::Value* finalResult = callResult;

        if (callResult->getType() != expectedRetTy) {
            if (callResult->getType()->isIntegerTy() && expectedRetTy->isIntegerTy()) {
                finalResult
                    = ctx.Builder->CreateIntCast(callResult, expectedRetTy, true, "call_ret_cast");
            } else if (callResult->getType()->isPointerTy() && expectedRetTy->isStructTy()) {
                finalResult = callResult;
            }
        }

        if (llvm::Value* existing = ctx.resolveSymbol(inst.dst)) {
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

} // namespace maml