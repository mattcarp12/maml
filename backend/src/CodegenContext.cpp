#include "CodegenContext.hpp"

#include <llvm/Support/raw_ostream.h>

namespace maml {

// =========================================================================
// LLVM Stringification Helpers
// =========================================================================
std::string ErrorHandler::stringify(llvm::Value *val) {
  if (!val) return "null";
  std::string str;
  llvm::raw_string_ostream rso(str);
  val->print(rso);
  return str;
}

std::string ErrorHandler::stringify(llvm::Type *ty) {
  if (!ty) return "null";
  std::string str;
  llvm::raw_string_ostream rso(str);
  ty->print(rso);
  return str;
}

// =========================================================================
// Context-Aware Error Reporting
// =========================================================================
void ErrorHandler::report(std::string_view message) {
  hasError = true;

  // Use llvm::errs() which is tightly integrated with LLVM's internal diagnostics
  llvm::errs() << "\n\033[1;31m[MAML Codegen Error]\033[0m ";

  if (Ctx) {
    llvm::errs() << "In function '\033[1;33m" << Ctx->CurrentFunctionName << "\033[0m' near instruction '\033[1;36m"
                 << Ctx->CurrentInstructionName << "\033[0m':\n";
  }

  llvm::errs() << "  -> " << message << "\n\n";
}

void ErrorHandler::warn(std::string_view message) {
  llvm::errs() << "\033[1;33m[MAML Warning]\033[0m " << message << "\n";
}

void ErrorHandler::fatal(std::string_view message) {
  report(message);
  llvm::report_fatal_error(llvm::StringRef(message.data(), message.size()), false);
  // 'false' prevents LLVM from printing the massive C++ stack trace, keeping output clean.
}

// =========================================================================
// Context Initialization
// =========================================================================
CodegenContext::CodegenContext(const std::string &moduleName) {
  Module = std::make_unique<llvm::Module>(moduleName, Context);
  Builder = std::make_unique<llvm::IRBuilder<>>(Context);

  // Link the error handler to this specific context instance
  Error.setContext(this);
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

  if (HeapVars.count(std::string(name))) {
    llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
    return Builder->CreateLoad(ptrTy, symbol, std::string(name) + "_heap_addr");
  }

  return symbol;
}

}  // namespace maml