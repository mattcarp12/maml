#include <llvm/IR/Intrinsics.h>

#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"
#include "mir.h"
#include "token.h"

namespace maml {

// NOTE (Phase 1 scope): inst.operator_ (std::string) does not exist on the
// real mir::BinaryOpInst/UnaryOpInst — the field is `op` (a TokenType enum).
// This file is patched to compile against TokenType with the same
// comparisons as before, expressed via `tokenText`. The full switch-based
// dispatch (dropping tokenText entirely in favor of `switch (inst.op)`) is
// tracked as Phase 2 identifier/dispatch work and not done here.
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
    llvm::Value* result = nullptr;

    if (left->getType()->isPointerTy() && right->getType()->isIntegerTy()) {
        if (auto* cInt = llvm::dyn_cast<llvm::ConstantInt>(right); cInt && cInt->isZero())
            right = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(left->getType()));
    } else if (right->getType()->isPointerTy() && left->getType()->isIntegerTy()) {
        if (auto* cInt = llvm::dyn_cast<llvm::ConstantInt>(left); cInt && cInt->isZero())
            left = llvm::ConstantPointerNull::get(llvm::cast<llvm::PointerType>(right->getType()));
    }

    if (left->getType()->isIntegerTy() && right->getType()->isIntegerTy()) {
        if (left->getType() != right->getType()) {
            // Coerce the right operand to match the left operand's integer width.
            // (This will cleanly truncate your i64 `0` down to an i32 to match the discriminant)
            right = ctx.Builder->CreateSExtOrTrunc(right, left->getType(), "binop_cast");
        }
    }

    std::string op = tokenText(inst.op);

    if (op == "/" || op == "%") {
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
        if (op == "/")
            result = ctx.Builder->CreateSDiv(left, right, "divtmp");
        if (op == "%")
            result = ctx.Builder->CreateSRem(left, right, "modtmp");
    } else if (op == "+")
        result = ctx.Builder->CreateAdd(left, right, "addtmp");
    else if (op == "-")
        result = ctx.Builder->CreateSub(left, right, "subtmp");
    else if (op == "*")
        result = ctx.Builder->CreateMul(left, right, "multmp");
    else if (op == "==")
        result = ctx.Builder->CreateICmpEQ(left, right, "eqtmp");
    else if (op == "!=")
        result = ctx.Builder->CreateICmpNE(left, right, "neqtmp");
    else if (op == "<")
        result = ctx.Builder->CreateICmpSLT(left, right, "lttmp");
    else if (op == ">")
        result = ctx.Builder->CreateICmpSGT(left, right, "gttmp");
    else if (op == "<=")
        result = ctx.Builder->CreateICmpSLE(left, right, "letmp");
    else if (op == ">=")
        result = ctx.Builder->CreateICmpSGE(left, right, "getmp");

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

static void lowerTaskGetResult(CodegenContext& ctx, const mir::CallInst& inst)
{
    llvm::Value* futurePtr = evaluateValue(ctx, inst.arguments[0]);

    // Extract raw coroutine frame pointer from the {ptr, i1} wrapper
    llvm::StructType* futureStructTy = llvm::StructType::get(ctx.Context,
        { llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt1Ty(ctx.Context) });
    llvm::Value* framePtrAddr = ctx.Builder->CreateStructGEP(futureStructTy, futurePtr, 0);
    llvm::Value* hdl = ctx.Builder->CreateLoad(
        llvm::PointerType::getUnqual(ctx.Context), framePtrAddr, "raw_coro_hdl");

    // Pass the extracted raw frame pointer to coro.promise
    llvm::Function* promiseFn
        = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_promise);
    llvm::Value* align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(ctx.Context), 8);
    llvm::Value* from = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);

    llvm::Value* promisePtr = ctx.Builder->CreateCall(promiseFn, { hdl, align, from });

    llvm::Type* expectedTy = llvmTypeFor(ctx, inst.type);

    if (!expectedTy->isVoidTy()) {
        llvm::Value* typedPromise
            = ctx.Builder->CreatePointerCast(promisePtr, llvm::PointerType::getUnqual(ctx.Context));
        llvm::Value* res = ctx.Builder->CreateLoad(expectedTy, typedPromise, "coro.result");

        if (llvm::Value* existing = ctx.resolveSymbol(inst.dst)) {
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