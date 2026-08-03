#include "ProgramGenerator.hpp"

#include <llvm/IR/DerivedTypes.h>
#include <llvm/IR/Intrinsics.h>
#include <llvm/IR/Verifier.h>
#include <string>
#include <variant>
#include <vector>

#include "CodegenContext.hpp"
#include "StmtGenerator.hpp"
#include "TypeLowering.hpp"
#include "cfg.h"
#include "llvm/IR/Attributes.h"
#include "llvm/IR/BasicBlock.h"
#include "llvm/IR/Constant.h"
#include "llvm/IR/Constants.h"
#include "llvm/IR/Instructions.h"
#include "llvm/Support/raw_ostream.h"
#include "mir.h"
#include "sym.h"

namespace maml {

constexpr const char* CORO_RESUME_HELPER = "maml_coro_resume_helper";
constexpr const char* CORO_DONE_HELPER = "maml_coro_done_helper";
constexpr const char* CORO_DESTROY_HELPER = "maml_coro_destroy_helper";

void defineCoroHelperStubs(CodegenContext& ctx)
{
    auto& Module = ctx.Module;
    auto& Context = ctx.Context;
    llvm::Type* voidTy = llvm::Type::getVoidTy(Context);
    llvm::Type* ptrTy = llvm::PointerType::getUnqual(Context);
    llvm::Type* i1Ty = llvm::Type::getInt1Ty(Context);

    // Define the LLVM structure type matching your Future: { ptr, i1 }
    llvm::StructType* futureStructTy = llvm::StructType::get(Context, { ptrTy, i1Ty });
    (void)futureStructTy;

    auto defineIfMissing = [&](const char* name, llvm::FunctionType* FT, auto body) {
        llvm::Function* F = Module->getFunction(name);
        if (!F) {
            F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, name, *Module);
        } else if (!F->isDeclaration()) {
            return;
        }
        llvm::BasicBlock* BB = llvm::BasicBlock::Create(Context, "entry", F);
        ctx.Builder->SetInsertPoint(BB);
        body(F);
    };

    defineIfMissing(CORO_RESUME_HELPER, llvm::FunctionType::get(voidTy, { ptrTy }, false),
        [&](llvm::Function* F) {
            llvm::Value* hdl = F->getArg(0);
            llvm::Function* resumeFn
                = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_resume);
            ctx.Builder->CreateCall(resumeFn, { hdl });
            ctx.Builder->CreateRetVoid();
        });
    defineIfMissing(
        CORO_DONE_HELPER, llvm::FunctionType::get(i1Ty, { ptrTy }, false), [&](llvm::Function* F) {
            llvm::Value* hdl = F->getArg(0);
            llvm::Function* doneFn
                = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_done);
            llvm::Value* isDone = ctx.Builder->CreateCall(doneFn, { hdl });
            ctx.Builder->CreateRet(isDone);
        });
    defineIfMissing(CORO_DESTROY_HELPER, llvm::FunctionType::get(voidTy, { ptrTy }, false),
        [&](llvm::Function* F) {
            llvm::Value* hdl = F->getArg(0);
            llvm::Function* destroyFn
                = llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_destroy);
            ctx.Builder->CreateCall(destroyFn, { hdl });
            ctx.Builder->CreateRetVoid();
        });
}

static void allocateVariables(CodegenContext& ctx, llvm::Function* F, const mir::Function& fn)
{
    // 1. Allocate Arguments
    unsigned idx = 0;
    for (auto& arg : F->args()) {
        SymID paramName = fn.params[idx].name;
        std::string paramNameStr = std::string(ctx.Sym.resolve(paramName));
        arg.setName(paramNameStr);
        llvm::AllocaInst* alloca = ctx.Builder->CreateAlloca(arg.getType(), nullptr, paramNameStr);
        ctx.Builder->CreateStore(&arg, alloca);
        ctx.SymbolEnv.back()[paramName] = alloca;
        ctx.SymbolTypes[paramName] = arg.getType();
        idx++;
    }

    // 2. Allocate Locals
    for (const auto& [name, type] : fn.locals) {
        // Skip if already allocated (e.g., it was a parameter)
        if (ctx.SymbolEnv.back().count(name)) {
            continue;
        }
        llvm::Type* ty = llvmTypeFor(ctx, type);
        if (ty->isVoidTy())
            continue;

        std::string nameStr = std::string(ctx.Sym.resolve(name));
        ctx.SymbolTypes[name] = ty;
        llvm::AllocaInst* alloca = ctx.Builder->CreateAlloca(ty, nullptr, nameStr);
        ctx.Builder->CreateStore(llvm::Constant::getNullValue(ty), alloca);
        ctx.SymbolEnv.back()[name] = alloca;
    }
}

static void finalizeCoroutineBlocks(CodegenContext& ctx, llvm::Function* F)
{
    // --- Suspend Block (Return to Caller) ---
    ctx.Builder->SetInsertPoint(ctx.CoroSuspendBlock);
    llvm::Function* endFn
        = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_end);
    llvm::Value* isUnwind = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
    llvm::Value* noneToken = llvm::ConstantTokenNone::get(ctx.Context);
    ctx.Builder->CreateCall(endFn, { ctx.CurrentCoroHandle, isUnwind, noneToken });

    llvm::Type* retTy = F->getReturnType();
    if (retTy->isStructTy()) {
        llvm::Value* futureVal = llvm::UndefValue::get(retTy);
        futureVal = ctx.Builder->CreateInsertValue(futureVal, ctx.CurrentCoroHandle, 0);
        llvm::Value* isDone = llvm::ConstantInt::get(llvm::Type::getInt1Ty(ctx.Context), 0);
        futureVal = ctx.Builder->CreateInsertValue(futureVal, isDone, 1);
        ctx.Builder->CreateRet(futureVal);
    } else if (retTy->isVoidTy()) {
        ctx.Builder->CreateRetVoid();
    } else {
        ctx.Builder->CreateRet(ctx.CurrentCoroHandle);
    }

    // --- Cleanup Block (Destroy Coroutine) ---
    ctx.Builder->SetInsertPoint(ctx.CoroCleanupBlock);
    llvm::Function* freeFn
        = llvm::Intrinsic::getDeclaration(ctx.Module.get(), llvm::Intrinsic::coro_free);
    llvm::Value* memToFree = ctx.Builder->CreateCall(freeFn, { ctx.CoroId, ctx.CurrentCoroHandle });
    llvm::Function* mamlFreeFn = ctx.Module->getFunction("maml_free");
    ctx.Builder->CreateCall(mamlFreeFn, { memToFree });
    ctx.Builder->CreateBr(ctx.CoroSuspendBlock);
}

void declareFunction(CodegenContext& ctx, const mir::Function& fn)
{
    std::string fnName = std::string(ctx.Sym.resolve(fn.name));
    if (ctx.Module->getFunction(fnName)) {
        return;
    }
    llvm::Type* retType = llvm::Type::getVoidTy(ctx.Context);
    if (fn.returnType)
        retType = llvmTypeFor(ctx, fn.returnType);
    if (fnName == "main")
        retType = llvm::Type::getInt32Ty(ctx.Context);

    // Async functions must return a coroutine handle pointer (ptr)
    // even if their MAML return type is Unit/void.
    if (fn.isAsync) {
        retType = llvm::PointerType::getUnqual(ctx.Context);
    }

    std::vector<llvm::Type*> paramTypes;
    for (const auto& p : fn.params)
        paramTypes.push_back(llvmTypeFor(ctx, p.type));

    llvm::FunctionType* FT = llvm::FunctionType::get(retType, paramTypes, false);
    llvm::Function* F
        = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, fnName, *ctx.Module);
    if (fn.isAsync)
        F->addFnAttr(llvm::Attribute::PresplitCoroutine);
}

void compileFunction(CodegenContext& ctx, const mir::Function& fn)
{
    ctx.CurrentFunctionName = fn.name;
    std::string fnName = std::string(ctx.Sym.resolve(fn.name));

    // Reset coroutine context members between functions
    ctx.CurrentCoroHandle = nullptr;
    ctx.PromiseSlot = nullptr;
    ctx.CoroId = nullptr;
    ctx.CoroSuspendBlock = nullptr;
    ctx.CoroCleanupBlock = nullptr;

    // 1. Retrieve pre-declared signature
    llvm::Function* F = ctx.Module->getFunction(fnName);
    if (!F) {
        ctx.Error.fatal("Function not declared in pass 1: " + fnName);
    }

    // Extern functions only need declarations, no bodies
    if (fn.isExtern)
        return;

    ctx.pushScope();

    // 2. Setup Entry & Async Blocks
    llvm::BasicBlock* allocBB = llvm::BasicBlock::Create(ctx.Context, "entry_allocs", F);
    if (fn.isAsync) {
        ctx.CoroSuspendBlock = llvm::BasicBlock::Create(ctx.Context, "coro.suspend.ret", F);
        ctx.CoroCleanupBlock = llvm::BasicBlock::Create(ctx.Context, "coro.cleanup", F);
        if (fn.graph)
            ctx.CoroEntryBlockId = fn.graph->entry;
    }

    ctx.Builder->SetInsertPoint(allocBB);

    if (fnName == "main") {
        if (llvm::Function* initFn = ctx.Module->getFunction("maml_coro_runtime_init")) {
            ctx.Builder->CreateCall(initFn, {});
        }
    }

    // 3. Allocate Variables
    allocateVariables(ctx, F, fn);

    // 4. Map MIR Blocks
    if (fn.graph && !fn.graph->blocks.empty()) {
        std::vector<mir::BasicBlock*> sorted = fn.graph->sortedBlocks();

        // Pre-create all basic blocks
        for (mir::BasicBlock* block : sorted) {
            if (block->id == mir::InvalidBlock)
                ctx.Error.fatal("Encountered block with missing ID!");
            ctx.Blocks[block->id]
                = llvm::BasicBlock::Create(ctx.Context, "bb" + std::to_string(block->id), F);
        }

        // Isolate and compile the async prologue first
        if (fn.isAsync) {
            bool foundPrologue = false;
            for (mir::BasicBlock* block : sorted) {
                for (const auto& inst : block->statements) {
                    if (std::holds_alternative<mir::CoroPrologueInst>(inst)) {
                        compileInstruction(ctx, inst);
                        foundPrologue = true;
                        break;
                    }
                }
                if (foundPrologue)
                    break;
            }
        }

        if (fn.graph->entry != mir::InvalidBlock && !fn.isAsync) {
            ctx.Builder->CreateBr(ctx.Blocks[fn.graph->entry]);
        }

        // Compile Instructions & Terminators
        for (mir::BasicBlock* block : sorted) {
            ctx.Builder->SetInsertPoint(ctx.Blocks[block->id]);

            ctx.CurrentInstructionName = "Instructions (Block " + std::to_string(block->id) + ")";
            for (const auto& inst : block->statements) {
                if (!std::holds_alternative<mir::CoroPrologueInst>(inst)) {
                    compileInstruction(ctx, inst);
                }
            }

            ctx.CurrentInstructionName = "Terminator (Block " + std::to_string(block->id) + ")";
            compileTerminator(ctx, block->terminator);
        }
    } else {
        ctx.Builder->CreateRetVoid();
    }

    // 5. Finalize Async State
    if (fn.isAsync) {
        finalizeCoroutineBlocks(ctx, F);
    }

    ctx.popScope();
    ctx.Blocks.clear();

    if (llvm::verifyFunction(*F, &llvm::errs())) {
        ctx.Error.fatal("LLVM Verification failed for function: " + fnName);
    }
}

void compileProgram(CodegenContext& ctx, const mir::Program& prog)
{
    defineCoroHelperStubs(ctx);

    // Pass 1: Declare all function signatures
    for (const auto& fn : prog.functions) {
        declareFunction(ctx, fn);
    }

    // Pass 2: Compile function bodies
    for (const auto& fn : prog.functions) {
        compileFunction(ctx, fn);
    }

    // --- Final Module Verification ---
    if (llvm::verifyModule(*ctx.Module, &llvm::errs())) {
        ctx.Error.fatal(
            "LLVM Module Verification failed! Check the console output above for details.");
    }
}

} // namespace maml