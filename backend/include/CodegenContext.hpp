#pragma once

#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>

#include <memory>
#include <string>
#include <string_view>
#include <unordered_map>
#include <unordered_set>
#include <vector>

namespace maml {

// Forward declaration so ErrorHandler can access context tracking
class CodegenContext;

class ErrorHandler {
  bool hasError = false;
  CodegenContext *Ctx = nullptr;

 public:
  void setContext(CodegenContext *c) { Ctx = c; }

  void report(std::string_view message);
  void fatal(std::string_view message);
  void warn(std::string_view message);

  bool hasErrors() const { return hasError; }

  // Vital LLVM Stringification Helpers
  static std::string stringify(llvm::Value *val);
  static std::string stringify(llvm::Type *ty);
};

struct StringViewHash {
  using is_transparent = void;
  std::size_t operator()(std::string_view sv) const { return std::hash<std::string_view>{}(sv); }
};

template <typename V>
using FastMap = std::unordered_map<std::string, V, StringViewHash, std::equal_to<>>;

template <typename V>
using ViewMap = std::unordered_map<std::string_view, V>;

class CodegenContext {
 public:
  llvm::LLVMContext Context;
  std::unique_ptr<llvm::Module> Module;
  std::unique_ptr<llvm::IRBuilder<>> Builder;
  ErrorHandler Error;

  // --- Observability & Tracking ---
  std::string CurrentFunctionName = "<top-level>";
  std::string CurrentInstructionName = "<unknown>";

  ViewMap<llvm::Value *> SymbolTable;
  std::vector<FastMap<llvm::Value *>> SymbolEnv;
  std::unordered_map<int, llvm::BasicBlock *> Blocks;
  std::vector<llvm::BasicBlock *> LoopExitStack;

  std::unordered_set<std::string> HeapVars;
  std::unordered_map<std::string, llvm::Type *> SymbolTypes;
  llvm::Value *getMemoryBase(std::string_view name);

  llvm::Value *CurrentCoroHandle = nullptr;
  llvm::Value *PromiseSlot = nullptr;
  llvm::Value *CoroId = nullptr;
  llvm::BasicBlock *CoroSuspendBlock = nullptr;
  llvm::BasicBlock *CoroCleanupBlock = nullptr;
  int CoroEntryBlockId = 0;

  CodegenContext(const std::string &moduleName);
  void pushScope();
  void popScope();
  llvm::Value *resolveSymbol(std::string_view name);
};

}  // namespace maml