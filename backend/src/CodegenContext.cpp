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
  llvm::errs() << "\n[MAML Codegen Error] ";

  if (Ctx) {
    llvm::errs() << "In function '" << Ctx->CurrentFunctionName << "' near instruction '" << Ctx->CurrentInstructionName
                 << "':\n";
  }

  llvm::errs() << "  -> " << message << "\n\n";
}

void ErrorHandler::warn(std::string_view message) { llvm::errs() << "[MAML Warning] " << message << "\n"; }

void ErrorHandler::fatal(std::string_view message) {
  report(message);
  llvm::report_fatal_error(llvm::StringRef(message.data(), message.size()), false);
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
  return symbol;
}

}  // namespace maml