#ifndef MAML_CODEGEN_CONTEXT_H
#define MAML_CODEGEN_CONTEXT_H

#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>

#include <memory>
#include <string>
#include <string_view>
#include <unordered_map>
#include <vector>

namespace maml {

class ErrorHandler {
  bool hasError = false;

 public:
  void report(std::string_view message);
  void fatal(std::string_view message);
  bool hasErrors() const { return hasError; }
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

  FastMap<llvm::Type *> TypeCache;
  ViewMap<llvm::Value *> SymbolTable;
  std::vector<FastMap<llvm::Value *>> SymbolEnv;
  std::unordered_map<int, llvm::BasicBlock *> Blocks;
  std::vector<llvm::BasicBlock *> LoopExitStack;

  CodegenContext(const std::string &moduleName);
  void pushScope();
  void popScope();
  llvm::Value *resolveSymbol(std::string_view name);
};

}  // namespace maml

#endif