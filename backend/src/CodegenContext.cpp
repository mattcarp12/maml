#include "CodegenContext.h"

namespace maml {

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
      return found->second; // use the iterator directly — operator[] doesn't
    } // support transparent lookup, find() does
  }
  return nullptr;
}

} // namespace maml