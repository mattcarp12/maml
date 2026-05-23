#ifndef MAML_CODEGEN_CONTEXT_H
#define MAML_CODEGEN_CONTEXT_H

#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>
#include <memory>
#include <nlohmann/json.hpp>
#include <string>
#include <string_view>
#include <unordered_map>
#include <vector>

#include "ErrorHandler.h"

namespace maml {

struct StringViewHash {
  using is_transparent = void;
  std::size_t operator()(std::string_view sv) const {
    return std::hash<std::string_view>{}(sv);
  }
};

// Owns its string keys; supports heterogeneous string_view lookup.
template <typename V>
using FastMap =
    std::unordered_map<std::string, V, StringViewHash, std::equal_to<>>;

// Keys must be string_views into storage that outlives this map.
// Only use for tables populated from static/constexpr data (e.g. rt::
// constants).
template <typename V> using ViewMap = std::unordered_map<std::string_view, V>;

struct TrackedItem {
  llvm::Value *ptr;
  bool isRaw;
  nlohmann::json typeJson;
};

class CodegenContext {
public:
  llvm::LLVMContext Context;
  std::unique_ptr<llvm::Module> Module;
  std::unique_ptr<llvm::IRBuilder<>> Builder;

  ErrorHandler Error;

  FastMap<llvm::Type *> TypeCache;
  ViewMap<llvm::Value *>
      SymbolTable; // keyed by rt:: constexpr strings — safe as ViewMap
  std::vector<FastMap<llvm::Value *>>
      SymbolEnv; // owns its keys — was ViewMap, latent UAF
  std::vector<std::vector<TrackedItem>> ScopeStack;

  CodegenContext(const std::string &moduleName);

  void pushScope();
  void popScope(llvm::FunctionCallee releaseFn);
  llvm::Value *resolveSymbol(std::string_view name);
};

} // namespace maml

#endif // MAML_CODEGEN_CONTEXT_H