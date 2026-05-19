#include <iostream>
#include <fstream>
#include <unordered_map>
#include <string>
#include <vector>
#include <nlohmann/json.hpp>

#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>
#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/Verifier.h>
#include <llvm/IR/Intrinsics.h>

using json = nlohmann::json;

class CodegenBackend {
public:
    llvm::LLVMContext Context;
    std::unique_ptr<llvm::Module> Module;
    std::unique_ptr<llvm::IRBuilder<>> Builder;

    // Symbol table to map variable names to live LLVM values
    std::unordered_map<std::string, llvm::Value*> SymbolTable;

    CodegenBackend(const std::string& moduleName) {
        Module = std::make_unique<llvm::Module>(moduleName, Context);
        Builder = std::make_unique<llvm::IRBuilder<>>(Context);
    }

    llvm::Type* llvmTypeFor(const json& typeJson) {
        if (typeJson.is_null()) return llvm::Type::getVoidTy(Context);
        
        std::string kind = typeJson["kind"];
        if (kind == "Int")    return llvm::Type::getInt32Ty(Context);
        if (kind == "Bool")   return llvm::Type::getInt1Ty(Context);
        if (kind == "String") {
            return llvm::StructType::get(Context, {
                llvm::PointerType::getUnqual(Context),
                llvm::Type::getInt32Ty(Context)
            });
        }
        if (kind == "Task")   return llvm::PointerType::getUnqual(Context); // Handles are i8* pointers
        return llvm::Type::getInt32Ty(Context);
    }

    // =========================================================================
    // EXPRESSION GENERATOR
    // =========================================================================
    llvm::Value* evaluateExpression(const json& expr) {
        if (expr.is_null()) return nullptr;

        std::string nodeType = expr["node_type"];

        if (nodeType == "IntLiteral") {
            int value = expr["value"];
            return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), value);
        }

        if (nodeType == "BoolLiteral") {
            bool value = expr["value"];
            return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), value ? 1 : 0);
        }

        if (nodeType == "Identifier") {
            std::string varName = expr["value"];
            if (SymbolTable.find(varName) == SymbolTable.end()) {
                std::cerr << "Backend Error: Undefined variable reference: " << varName << "\n";
                return nullptr;
            }
            llvm::Value* val = SymbolTable[varName];
            if (llvm::isa<llvm::AllocaInst>(val)) {
                llvm::Type* allocatedType = llvmTypeFor(expr["maml_type"]);
                return Builder->CreateLoad(allocatedType, val, varName);
            }
            return val;
        }

        if (nodeType == "InfixExpr") {
            llvm::Value* left = evaluateExpression(expr["left"]);
            llvm::Value* right = evaluateExpression(expr["right"]);
            std::string op = expr["operator"];

            if (op == "+")  return Builder->CreateAdd(left, right, "addtmp");
            if (op == "-")  return Builder->CreateSub(left, right, "subtmp");
            if (op == "*")  return Builder->CreateMul(left, right, "multmp");
            if (op == "==") return Builder->CreateICmpEQ(left, right, "eqtmp");
            if (op == "<")  return Builder->CreateICmpSLT(left, right, "lttmp");
        }

        if (nodeType == "AwaitExpr") {
            return compileAwaitExpression(expr);
        }

        if (nodeType == "CallExpr") {
            if (expr["function"]["node_type"] == "Identifier") {
                std::string funcName = expr["function"]["value"];
                
                // Intercept Compiler Concurrency Built-ins
                if (funcName == "spawn") {
                    llvm::Value* taskArg = evaluateExpression(expr["arguments"][0]["value"]);
                    llvm::FunctionCallee spawnFn = Module->getOrInsertFunction(
                        "maml_spawn_task", 
                        llvm::Type::getVoidTy(Context), 
                        llvm::PointerType::getUnqual(Context)
                    );
                    Builder->CreateCall(spawnFn, { taskArg });
                    return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
                }
                if (funcName == "run_executor") {
                    llvm::FunctionCallee runFn = Module->getOrInsertFunction(
                        "maml_run_executor", 
                        llvm::Type::getVoidTy(Context)
                    );
                    Builder->CreateCall(runFn, {});
                    return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
                }

                // Standard Function Call Translation
                llvm::Function* calleeFn = Module->getFunction(funcName);
                if (!calleeFn) {
                    std::vector<llvm::Type*> argTypes;
                    for (const auto& arg : expr["arguments"]) {
                        argTypes.push_back(llvmTypeFor(arg["value"]["maml_type"]));
                    }
                    llvm::Type* retType = llvmTypeFor(expr["maml_type"]);
                    llvm::FunctionType* FT = llvm::FunctionType::get(retType, argTypes, false);
                    calleeFn = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, funcName, *Module);
                }

                std::vector<llvm::Value*> argsV;
                for (const auto& arg : expr["arguments"]) {
                    argsV.push_back(evaluateExpression(arg["value"]));
                }
                return Builder->CreateCall(calleeFn, argsV, "calltmp");
            }
        }

        std::cerr << "Backend Error: Unsupported expression type: " << nodeType << "\n";
        return nullptr;
    }

    // =========================================================================
    // STATEMENT GENERATOR
    // =========================================================================
    void compileStatement(const json& stmt) {
        if (stmt.is_null()) return;

        std::string nodeType = stmt["node_type"];

        if (nodeType == "BlockStmt") {
            for (const auto& s : stmt["statements"]) {
                compileStatement(s);
            }
            return;
        }

        if (nodeType == "DeclareStmt") {
            std::string name = stmt["name"];
            llvm::Value* initialVal = evaluateExpression(stmt["value"]);
            
            llvm::Type* llvmTy = llvmTypeFor(stmt["maml_type"]);
            llvm::AllocaInst* alloca = Builder->CreateAlloca(llvmTy, nullptr, name);
            
            if (initialVal) {
                Builder->CreateStore(initialVal, alloca);
            }
            SymbolTable[name] = alloca;
            return;
        }

        if (nodeType == "AssignStmt") {
            llvm::Value* rvalue = evaluateExpression(stmt["rvalue"]);
            if (stmt["lvalue"]["node_type"] == "Identifier") {
                std::string varName = stmt["lvalue"]["value"];
                Builder->CreateStore(rvalue, SymbolTable[varName]);
            }
            return;
        }

        if (nodeType == "ForStmt") {
            compileForLoop(stmt);
            return;
        }

        if (nodeType == "ReturnStmt") {
            llvm::Value* retVal = evaluateExpression(stmt["value"]);
            if (SymbolTable.find("__coro_hdl") != SymbolTable.end()) {
                llvm::Value* coroHdl = SymbolTable["__coro_hdl"];
                Builder->CreateRet(coroHdl);
            } else {
                if (retVal) {
                    Builder->CreateRet(retVal);
                } else {
                    Builder->CreateRetVoid();
                }
            }
            return;
        }
    }

    // =========================================================================
    // COROUTINE PROLOGUE BUILDER (Linear, Phi-Free Allocation Flow)
    // =========================================================================
    void compileCoroutinePrologue(llvm::Function* F) {
        llvm::Value* align = llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
        llvm::Value* nullPtr = llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));
        
        // 1. Declare and issue @llvm.coro.id
        llvm::Function* coroIdFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_id);
        llvm::Value* coroId = Builder->CreateCall(coroIdFn, { align, nullPtr, nullPtr, nullPtr }, "coro.id");
        SymbolTable["__coro_id"] = coroId;

        // 2. Fetch required frame layout allocation metrics
        llvm::Function* coroSizeFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_size, { llvm::Type::getInt32Ty(Context) });
        llvm::Value* coroSize = Builder->CreateCall(coroSizeFn, {}, "coro.size");

        // 3. Request a freestanding memory frame heap slot via your runtime allocation library
        llvm::FunctionCallee mamlAlloc = Module->getOrInsertFunction(
            "maml_alloc", 
            llvm::FunctionType::get(llvm::PointerType::getUnqual(Context), { llvm::Type::getInt32Ty(Context) }, false)
        );
        llvm::Value* allocMem = Builder->CreateCall(mamlAlloc, { coroSize }, "coro.alloc.mem");

        // 4. Anchor state frame boundaries and record the returning token handle
        llvm::Function* coroBeginFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_begin);
        llvm::Value* coroHdl = Builder->CreateCall(coroBeginFn, { coroId, allocMem }, "coro.hdl");
        SymbolTable["__coro_hdl"] = coroHdl;
    }

    void compileFunction(const json& fn) {
        std::string name = fn["name"];
        
        llvm::Type* retType = llvm::Type::getVoidTy(Context);
        if (fn.contains("maml_type") && fn["maml_type"].contains("return")) {
            retType = llvmTypeFor(fn["maml_type"]["return"]);
        }

        std::vector<llvm::Type*> paramTypes;
        if (fn.contains("params")) {
            for (const auto& p : fn["params"]) {
                paramTypes.push_back(llvmTypeFor(p["type"]));
            }
        }

        llvm::FunctionType* FT = llvm::FunctionType::get(retType, paramTypes, false);
        llvm::Function* F = llvm::Function::Create(FT, llvm::Function::ExternalLinkage, name, *Module);

        if (fn.contains("is_async") && fn["is_async"] == true) {
            F->addFnAttr(llvm::Attribute::PresplitCoroutine);
        }

        llvm::BasicBlock* BB = llvm::BasicBlock::Create(Context, "entry", F);
        Builder->SetInsertPoint(BB);

        SymbolTable.clear();

        // Fire up coroutine orchestration structures before processing statements
        if (fn.contains("is_async") && fn["is_async"] == true) {
            compileCoroutinePrologue(F);
        }

        // Map function parameters onto the local stack frame variables
        unsigned Idx = 0;
        for (auto& Arg : F->args()) {
            std::string pName = fn["params"][Idx]["name"];
            llvm::Type* pType = paramTypes[Idx];
            llvm::AllocaInst* alloca = Builder->CreateAlloca(pType, nullptr, pName);
            Builder->CreateStore(&Arg, alloca);
            SymbolTable[pName] = alloca;
            Idx++;
        }

        if (fn.contains("body")) {
            compileStatement(fn["body"]);
        }

        // Catch-all fallthrough terminator to guarantee module validity
        if (!Builder->GetInsertBlock()->getTerminator()) {
            if (SymbolTable.find("__coro_hdl") != SymbolTable.end()) {
                llvm::Value* coroHdl = SymbolTable["__coro_hdl"];
                Builder->CreateRet(coroHdl);
            } else if (F->getReturnType()->isVoidTy()) {
                Builder->CreateRetVoid();
            } else {
                Builder->CreateRet(llvm::Constant::getNullValue(F->getReturnType()));
            }
        }
    }

    void compileForLoop(const json& s);
    llvm::Value* compileAwaitExpression(const json& e);
    void compileProgram(const json& ast);
};

void CodegenBackend::compileForLoop(const json& s) {
    llvm::Function* parentFn = Builder->GetInsertBlock()->getParent();

    llvm::BasicBlock* condBB = llvm::BasicBlock::Create(Context, "for.cond", parentFn);
    llvm::BasicBlock* bodyBB = llvm::BasicBlock::Create(Context, "for.body", parentFn);
    llvm::BasicBlock* loopExitBB = llvm::BasicBlock::Create(Context, "for.exit", parentFn);

    if (s.contains("init") && !s["init"].is_null()) {
        compileStatement(s["init"]);
    }
    Builder->CreateBr(condBB);

    Builder->SetInsertPoint(condBB);
    if (s.contains("condition") && !s["condition"].is_null()) {
        llvm::Value* condVal = evaluateExpression(s["condition"]);
        Builder->CreateCondBr(condVal, bodyBB, loopExitBB);
    } else {
        Builder->CreateBr(bodyBB);
    }

    Builder->SetInsertPoint(bodyBB);
    if (s.contains("body") && !s["body"].is_null()) {
        compileStatement(s["body"]);
    }

    if (s.contains("post") && !s["post"].is_null()) {
        compileStatement(s["post"]);
    }
    Builder->CreateBr(condBB);

    Builder->SetInsertPoint(loopExitBB);
}

llvm::Value* CodegenBackend::compileAwaitExpression(const json& e) {
    llvm::Value* taskVal = evaluateExpression(e["value"]);
    llvm::Function* parentFn = Builder->GetInsertBlock()->getParent();

    llvm::BasicBlock* resumeBB = llvm::BasicBlock::Create(Context, "await.resume", parentFn);
    llvm::BasicBlock* cleanupBB = llvm::BasicBlock::Create(Context, "await.cleanup", parentFn);
    llvm::BasicBlock* suspendBB = llvm::BasicBlock::Create(Context, "await.suspend", parentFn);

    llvm::Function* coroSuspendFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_suspend);

    llvm::Value* noneToken = llvm::ConstantTokenNone::get(Context);
    llvm::Value* isFinalValue = llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0);
    
    llvm::Value* suspendResult = Builder->CreateCall(coroSuspendFn, { noneToken, isFinalValue }, "suspend_res");

    llvm::SwitchInst* sw = Builder->CreateSwitch(suspendResult, suspendBB, 2);
    sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0), resumeBB);
    sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1), cleanupBB);

    Builder->SetInsertPoint(cleanupBB);
    llvm::Function* coroFreeFn = llvm::Intrinsic::getOrInsertDeclaration(Module.get(), llvm::Intrinsic::coro_free);
    llvm::Value* freeMem = Builder->CreateCall(coroFreeFn, { SymbolTable["__coro_id"], SymbolTable["__coro_hdl"] });
    Builder->CreateBr(suspendBB);

    Builder->SetInsertPoint(suspendBB);
    Builder->CreateRet(SymbolTable["__coro_hdl"]);

    Builder->SetInsertPoint(resumeBB);
    return taskVal; 
}

void CodegenBackend::compileProgram(const json& ast) {
    if (ast["node_type"] != "Program") return;
    
    llvm::Function::Create(
        llvm::FunctionType::get(llvm::Type::getVoidTy(Context), { llvm::PointerType::getUnqual(Context) }, false),
        llvm::Function::ExternalLinkage, "llvm.coro.resume", *Module
    );

    for (const auto& decl : ast["decls"]) {
        if (decl["node_type"] == "FnDecl") {
            compileFunction(decl); // FIXED: Correctly routed function emitter
        }
    }
}

int main(int argc, char* argv[]) {
    if (argc < 2) {
        std::cerr << "Usage: maml_cc <ast_json_file>\n";
        return 1;
    }

    std::ifstream f(argv[1]);
    if (!f.is_open()) {
        std::cerr << "Could not open target input JSON file.\n";
        return 1;
    }
    
    json ast = json::parse(f);

    CodegenBackend backend("maml_core_module");
    backend.compileProgram(ast);

    if (llvm::verifyModule(*backend.Module, &llvm::errs())) {
        std::cerr << "\nBackend Error: Internal generated LLVM IR module contains structural verification errors!\n";
        return 1;
    }

    backend.Module->print(llvm::outs(), nullptr);
    return 0;
}