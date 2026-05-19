#include <fstream>
#include <iostream>
#include <nlohmann/json.hpp>
#include <string>
#include <unordered_map>

#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/Intrinsics.h>
#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>
#include <llvm/IR/Verifier.h>

using json = nlohmann::json;

class CodegenBackend {
public:
  llvm::LLVMContext Context;
  std::unique_ptr<llvm::Module> Module;
  std::unique_ptr<llvm::IRBuilder<>> Builder;

  // Symbol table to map variable names to live LLVM values
  std::unordered_map<std::string, llvm::Value *> SymbolTable;

  CodegenBackend(const std::string &moduleName) {
    Module = std::make_unique<llvm::Module>(moduleName, Context);
    Builder = std::make_unique<llvm::IRBuilder<>>(Context);
  }

  llvm::Type *llvmTypeFor(const json &typeJson) {
    if (typeJson.is_null())
      return llvm::Type::getVoidTy(Context);

    std::string kind = typeJson["kind"];
    if (kind == "Int")
      return llvm::Type::getInt32Ty(Context);
    if (kind == "Bool")
      return llvm::Type::getInt1Ty(Context);
    if (kind == "String") {
      return llvm::StructType::get(Context,
                                   {llvm::PointerType::getUnqual(Context),
                                    llvm::Type::getInt32Ty(Context)});
    }
    if (kind == "Task")
      return llvm::PointerType::getUnqual(Context); // Handles are i8* pointers
    return llvm::Type::getInt32Ty(Context);
  }

  // =========================================================================
  // EXPRESSION GENERATOR
  // =========================================================================
  llvm::Value *evaluateExpression(const json &expr) {
    if (expr.is_null())
      return nullptr;

    std::string nodeType = expr["node_type"];

    if (nodeType == "IntLiteral") {
      int value = expr["value"];
      return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), value);
    }

    if (nodeType == "BoolLiteral") {
      bool value = expr["value"];
      return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context),
                                    value ? 1 : 0);
    }

    if (nodeType == "Identifier") {
      std::string varName = expr["value"];
      if (SymbolTable.find(varName) == SymbolTable.end()) {
        std::cerr << "Backend Error: Undefined variable reference: " << varName
                  << "\n";
        return nullptr;
      }
      // If the variable was allocated via Alloca, load the live value
      llvm::Value *val = SymbolTable[varName];
      if (llvm::isa<llvm::AllocaInst>(val)) {
        llvm::Type *allocatedType = llvmTypeFor(expr["maml_type"]);
        return Builder->CreateLoad(allocatedType, val, varName);
      }
      return val;
    }

    if (nodeType == "InfixExpr") {
      llvm::Value *left = evaluateExpression(expr["left"]);
      llvm::Value *right = evaluateExpression(expr["right"]);
      std::string op = expr["operator"];

      if (op == "+")
        return Builder->CreateAdd(left, right, "addtmp");
      if (op == "-")
        return Builder->CreateSub(left, right, "subtmp");
      if (op == "*")
        return Builder->CreateMul(left, right, "multmp");
      if (op == "==")
        return Builder->CreateICmpEQ(left, right, "eqtmp");
      if (op == "<")
        return Builder->CreateICmpSLT(left, right, "lttmp");
      // Expand with remaining operators as needed...
    }

    if (nodeType == "AwaitExpr") {
      return compileAwaitExpression(expr);
    }

    std::cerr << "Backend Error: Unsupported expression type: " << nodeType
              << "\n";
    return nullptr;
  }

  // =========================================================================
  // STATEMENT GENERATOR
  // =========================================================================
  void compileStatement(const json &stmt) {
    if (stmt.is_null())
      return;

    std::string nodeType = stmt["node_type"];

    if (nodeType == "BlockStmt") {
      for (const auto &s : stmt["statements"]) {
        compileStatement(s);
      }
      return;
    }

    if (nodeType == "DeclareStmt") {
      std::string name = stmt["name"];
      llvm::Value *initialVal = evaluateExpression(stmt["value"]);

      // Generate Alloca storage on the function stack frame
      llvm::Type *llvmTy = llvmTypeFor(stmt["maml_type"]);
      llvm::AllocaInst *alloca = Builder->CreateAlloca(llvmTy, nullptr, name);

      Builder->CreateStore(initialVal, alloca);
      SymbolTable[name] = alloca;
      return;
    }

    if (nodeType == "AssignStmt") {
      llvm::Value *rvalue = evaluateExpression(stmt["rvalue"]);
      // Simple variable assignment matching identifier targets
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
      llvm::Value *retVal = evaluateExpression(stmt["value"]);

      // Coroutine rule: If we are generating an async function,
      // we must redirect the return statement to yield the coroutine handle
      // instead!
      if (SymbolTable.find("__coro_hdl") != SymbolTable.end()) {
        llvm::Value *coroHdl = SymbolTable["__coro_hdl"];
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
  // SPECIFIC TRANSLATION UNITS (LOOPS & COROUTINES)
  // =========================================================================
  void compileForLoop(const json &s);
  llvm::Value *compileAwaitExpression(const json &e);
  void compileProgram(const json &ast);
};

void CodegenBackend::compileForLoop(const json &s) {
  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  // Create execution branches
  llvm::BasicBlock *condBB =
      llvm::BasicBlock::Create(Context, "for.cond", parentFn);
  llvm::BasicBlock *bodyBB =
      llvm::BasicBlock::Create(Context, "for.body", parentFn);
  llvm::BasicBlock *loopExitBB =
      llvm::BasicBlock::Create(Context, "for.exit", parentFn);

  // 1. Run Init Statement (e.g., let i = 0)
  if (!s["init"].is_null()) {
    compileStatement(s["init"]);
  }
  Builder->CreateBr(condBB);

  // 2. Condition Evaluation Block
  Builder->SetInsertPoint(condBB);
  if (!s["condition"].is_null()) {
    llvm::Value *condVal = evaluateExpression(s["condition"]);
    Builder->CreateCondBr(condVal, bodyBB, loopExitBB);
  } else {
    Builder->CreateBr(
        bodyBB); // Loop defaults to infinite loop without boundary condition
  }

  // 3. Body Execution Block
  Builder->SetInsertPoint(bodyBB);
  compileStatement(s["body"]);

  // 4. Run Post Step Statement (e.g., i = i + 1)
  if (!s["post"].is_null()) {
    compileStatement(s["post"]);
  }
  Builder->CreateBr(condBB); // Return back up to re-evaluate the edge condition

  // 5. Shift context point past loop boundary
  Builder->SetInsertPoint(loopExitBB);
}

llvm::Value *CodegenBackend::compileAwaitExpression(const json &e) {
  // 1. Evaluate the future/task expression we're pausing on
  llvm::Value *taskVal = evaluateExpression(e["value"]);

  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  // Build unique block pathways matching the coroutine structural layout spec
  llvm::BasicBlock *resumeBB =
      llvm::BasicBlock::Create(Context, "await.resume", parentFn);
  llvm::BasicBlock *cleanupBB =
      llvm::BasicBlock::Create(Context, "await.cleanup", parentFn);
  llvm::BasicBlock *suspendBB =
      llvm::BasicBlock::Create(Context, "await.suspend", parentFn);

  // 2. Locate the native @llvm.coro.suspend intrinsic signature
  llvm::Function *coroSuspendFn = llvm::Intrinsic::getOrInsertDeclaration(
      Module.get(), llvm::Intrinsic::coro_suspend);

  // Arg 0: save token (none/null token), Arg 1: isFinal flag (false)
  llvm::Value *noneToken = llvm::ConstantTokenNone::get(Context);
  llvm::Value *isFinalValue =
      llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0);

  llvm::Value *suspendResult = Builder->CreateCall(
      coroSuspendFn, {noneToken, isFinalValue}, "suspend_res");

  // 3. Evaluate the result status code returned by the intrinsic frame logic
  // Value 0: Resume execution path
  // Value 1: Forced cleanup pathway (task killed prematurely)
  // Value -1: Suspend and drop execution path back up to runtime event loop
  llvm::SwitchInst *sw = Builder->CreateSwitch(suspendResult, suspendBB, 2);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0),
              resumeBB);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1),
              cleanupBB);

  // --- CLEANUP BRANCH ---
  Builder->SetInsertPoint(cleanupBB);
  llvm::Function *coroFreeFn = llvm::Intrinsic::getOrInsertDeclaration(
      Module.get(), llvm::Intrinsic::coro_free);
  llvm::Value *freeMem = Builder->CreateCall(
      coroFreeFn, {SymbolTable["__coro_id"], SymbolTable["__coro_hdl"]});

  // Call the application runtime free hook to release heap block allocations
  // safely (Assuming maml_free hook declaration wrapper exists)
  // Builder->CreateCall(MamlFreeFunc, { freeMem });
  Builder->CreateBr(suspendBB);

  // --- SUSPEND BRANCH ---
  Builder->SetInsertPoint(suspendBB);
  // Explicit return of the live state handle pointer back into the Zig
  // cooperative scheduler loop!
  Builder->CreateRet(SymbolTable["__coro_hdl"]);

  // --- RESUME BRANCH ---
  Builder->SetInsertPoint(resumeBB);

  // Yield back out the expected unwrapped internal type result to fulfill
  // remaining expressions
  return taskVal;
}

void CodegenBackend::compileProgram(const json &ast) {
  if (ast["node_type"] != "Program")
    return;

  // Setup and declare standard coroutine intrinsics inside the active module
  // space
  llvm::Function::Create(
      llvm::FunctionType::get(llvm::Type::getVoidTy(Context),
                              {llvm::PointerType::getUnqual(Context)}, false),
      llvm::Function::ExternalLinkage, "llvm.coro.resume", *Module);

  for (const auto &decl : ast["decls"]) {
    if (decl["node_type"] == "FnDecl") {
      compileStatement(decl); // Dispatch directly to statement walker
    }
  }
}

int main(int argc, char *argv[]) {
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

  // Verify structural module accuracy to catch structural compilation mistakes
  // early
  if (llvm::verifyModule(*backend.Module, &llvm::errs())) {
    std::cerr << "\nBackend Error: Internal generated LLVM IR module contains "
                 "structural verification errors!\n";
    return 1;
  }

  backend.Module->print(llvm::outs(), nullptr);
  return 0;
}