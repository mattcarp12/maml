#include "CodegenContext.hpp"

#include <iostream>

namespace maml {

void ErrorHandler::report(std::string_view message) {
  hasError = true;
  std::cerr << "Semantic Error: " << message << "\n";
}

void ErrorHandler::fatal(std::string_view message) {
  report(message);
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
    if (found != it->end()) return found->second;
  }
  return nullptr;
}

llvm::Value *CodegenContext::getMemoryBase(std::string_view name) {
  llvm::Value *symbol = resolveSymbol(name);
  if (!symbol) {
    Error.fatal("Attempted to access undefined variable: " + std::string(name));
    return nullptr;
  }

  // If the variable was promoted to the heap, our local symbol is just a pointer
  // to the heap address. We must load the actual heap address before accessing memory.
  if (HeapVars.count(std::string(name))) {
    llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
    return Builder->CreateLoad(ptrTy, symbol, std::string(name) + "_heap_addr");
  }

  // Memory is directly on the stack. Return the allocation pointer.
  return symbol;
}

}  // namespace maml