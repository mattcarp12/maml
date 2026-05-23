#include "CodegenContext.h"
#include "TypeLowering.h"

namespace maml {

CodegenContext::CodegenContext(const std::string &moduleName) {
  Module = std::make_unique<llvm::Module>(moduleName, Context);
  Builder = std::make_unique<llvm::IRBuilder<>>(Context);
}

void CodegenContext::pushScope() {
  ScopeStack.push_back({});
  SymbolEnv.push_back({});
}

void CodegenContext::popScope(llvm::FunctionCallee releaseFn) {
  auto &currentScope = ScopeStack.back();

  if (Builder->GetInsertBlock()->getTerminator() == nullptr) {
    for (auto it = currentScope.rbegin(); it != currentScope.rend(); ++it) {
      if (it->isRaw) {
        // On opaque-pointer LLVM, no cast is needed — pass the pointer
        // directly.
        Builder->CreateCall(releaseFn, {it->ptr});
      } else {
        // Use the stored typeJson to recover the correct struct type for the
        // GEP. it->ptr->getType() returns the opaque pointer type, not the
        // struct layout.
        llvm::Type *structTy = llvmTypeFor(*this, it->typeJson);
        llvm::Value *rawPtrGep = Builder->CreateGEP(
            structTy, it->ptr, {Builder->getInt32(0), Builder->getInt32(0)},
            "arc_scope_gep");
        llvm::Value *loadedRaw = Builder->CreateLoad(
            llvm::PointerType::getUnqual(Context), rawPtrGep, "arc_scope_load");
        Builder->CreateCall(releaseFn, {loadedRaw});
      }
    }
  }
  ScopeStack.pop_back();
  SymbolEnv.pop_back();
}

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