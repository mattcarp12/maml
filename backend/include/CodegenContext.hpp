#pragma once

#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>

#include <memory>
#include <string_view>
#include <unordered_map>
#include <vector>

#include "mir.h"
#include "sym.h"

namespace maml {

class CodegenContext;

class ErrorHandler {
    bool hasError = false;
    CodegenContext* Ctx = nullptr;

public:
    void setContext(CodegenContext* c) { Ctx = c; }
    void report(std::string_view message);
    void fatal(std::string_view message);
    void warn(std::string_view message);
    bool hasErrors() const { return hasError; }

    static std::string stringify(llvm::Value* val);
    static std::string stringify(llvm::Type* ty);
};

class CodegenContext {
public:
    llvm::LLVMContext Context;
    std::unique_ptr<llvm::Module> Module;
    std::unique_ptr<llvm::IRBuilder<>> Builder;
    ErrorHandler Error;
    SymbolTable& Sym; // Reference to frontend symbol table

    // --- Observability & Tracking ---
    SymID CurrentFunctionName = NoSymbol;
    std::string CurrentInstructionName = "<unknown>";

    // Extremely fast SymID (integer) lookups replacing the old string maps
    std::vector<std::unordered_map<SymID, llvm::Value*>> SymbolEnv;
    std::unordered_map<mir::BlockID, llvm::BasicBlock*> Blocks;
    std::unordered_map<SymID, llvm::Type*> SymbolTypes;

    llvm::Value* CurrentCoroHandle = nullptr;
    llvm::Value* PromiseSlot = nullptr;
    llvm::Value* CoroId = nullptr;
    llvm::BasicBlock* CoroSuspendBlock = nullptr;
    llvm::BasicBlock* CoroCleanupBlock = nullptr;
    mir::BlockID CoroEntryBlockId = mir::InvalidBlock;

    CodegenContext(const std::string& moduleName, SymbolTable& symTable);

    void pushScope();
    void popScope();
    llvm::Value* resolveSymbol(SymID name);
    llvm::Value* getMemoryBase(SymID name);
};

} // namespace maml