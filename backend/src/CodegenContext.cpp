#include "CodegenContext.h"

#include <iostream>

namespace maml {

void ErrorHandler::report(std::string_view message, const nlohmann::json &node) {
  hasError = true;
  std::cerr << "Semantic Error: " << message << "\n";
  if (!node.is_null()) {
    if (node.contains("line")) {
      std::cerr << "  at line " << node["line"];
      if (node.contains("column")) {
        std::cerr << ", column " << node["column"];
      }
      std::cerr << "\n";
    }
  }
}

void ErrorHandler::fatal(std::string_view message, const nlohmann::json &node) {
  report(message, node);
  // Explicitly wrap the string_view in a StringRef
  llvm::report_fatal_error(llvm::StringRef(message.data(), message.size()));
}

CodegenContext::CodegenContext(const std::string &moduleName) {
  Module = std::make_unique<llvm::Module>(moduleName, Context);
  Builder = std::make_unique<llvm::IRBuilder<>>(Context);
}

void CodegenContext::pushScope() { SymbolEnv.push_back({}); }
void CodegenContext::popScope() { SymbolEnv.pop_back(); }

llvm::Value *CodegenContext::resolveSymbol(std::string_view name) {
  for (auto it = SymbolEnv.rbegin(); it != SymbolEnv.rend(); ++it) {
    auto found = it->find(name);
    if (found != it->end()) {
      return found->second;  // use the iterator directly — operator[] doesn't
    }  // support transparent lookup, find() does
  }
  return nullptr;
}

}  // namespace maml