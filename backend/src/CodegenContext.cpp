#include "CodegenContext.hpp"
#include "sym.h"
#include "llvm/ADT/StringRef.h"
#include "llvm/IR/IRBuilder.h"
#include "llvm/IR/Module.h"
#include "llvm/IR/Type.h"
#include "llvm/IR/Value.h"
#include "llvm/Support/ErrorHandling.h"
#include <llvm/Support/raw_ostream.h>
#include <memory>
#include <string>
#include <string_view>

namespace maml {

std::string ErrorHandler::stringify(llvm::Value* val)
{
    if (!val)
        return "null";
    std::string str;
    llvm::raw_string_ostream rso(str);
    val->print(rso);
    return str;
}

std::string ErrorHandler::stringify(llvm::Type* ty)
{
    if (!ty)
        return "null";
    std::string str;
    llvm::raw_string_ostream rso(str);
    ty->print(rso);
    return str;
}

void ErrorHandler::report(std::string_view message)
{
    hasError = true;
    llvm::errs() << "\n[MAML Codegen Error] ";
    if (Ctx) {
        llvm::errs() << "In function '" << Ctx->Sym.resolve(Ctx->CurrentFunctionName)
                     << "' near instruction '" << Ctx->CurrentInstructionName << "':\n";
    }
    llvm::errs() << "  -> " << message << "\n\n";
}

void ErrorHandler::warn(std::string_view message)
{
    llvm::errs() << "[MAML Warning] " << message << "\n";
}

void ErrorHandler::fatal(std::string_view message)
{
    report(message);
    llvm::report_fatal_error(llvm::StringRef(message.data(), message.size()), false);
}

CodegenContext::CodegenContext(const std::string& moduleName, SymbolTable& symTable)
    : Sym(symTable)
{
    Module = std::make_unique<llvm::Module>(moduleName, Context);
    Builder = std::make_unique<llvm::IRBuilder<>>(Context);
    Error.setContext(this);
}

void CodegenContext::pushScope() { SymbolEnv.push_back({}); }
void CodegenContext::popScope() { SymbolEnv.pop_back(); }

llvm::Value* CodegenContext::resolveSymbol(SymID name)
{
    for (auto it = SymbolEnv.rbegin(); it != SymbolEnv.rend(); ++it) {
        auto found = it->find(name);
        if (found != it->end())
            return found->second;
    }
    return nullptr;
}

llvm::Value* CodegenContext::getMemoryBase(SymID name)
{
    llvm::Value* symbol = resolveSymbol(name);
    if (!symbol) {
        Error.fatal("Attempted to access undefined variable: " + std::string(Sym.resolve(name)));
        return nullptr;
    }
    return symbol;
}

} // namespace maml