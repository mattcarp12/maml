#include "MemoryManager.h"
#include "RuntimeConstants.h"
#include "TypeLowering.h"
#include <algorithm>

namespace maml {

bool MemoryManager::isHeapManagedType(const nlohmann::json &typeJson) {
  if (typeJson.is_null())
    return false;
  std::string_view kind = typeJson["kind"].get<std::string_view>();
  return kind == "String" || kind == "Vector" || kind == "Map" ||
         kind == "Task" || kind == "Slice";
}

bool MemoryManager::isRefType(const nlohmann::json &typeJson) {
  return isHeapManagedType(typeJson);
}

bool MemoryManager::needsARC(const nlohmann::json &typeJson) {
  if (isRefType(typeJson))
    return true;
  if (typeJson["kind"] == "Struct") {
    for (const auto &field : typeJson["fields"]) {
      if (needsARC(field["type"]))
        return true;
    }
  }
  return false;
}

llvm::Value *MemoryManager::extractHeapPointer(CodegenContext &ctx,
                                               llvm::Value *containerPtr,
                                               const nlohmann::json &typeJson) {
  llvm::Type *ty = llvmTypeFor(ctx, typeJson);
  llvm::Value *rawPtrGep = ctx.Builder->CreateGEP(
      ty, containerPtr, {ctx.Builder->getInt32(0), ctx.Builder->getInt32(0)},
      "arc_raw_gep");
  return ctx.Builder->CreateLoad(llvm::PointerType::getUnqual(ctx.Context),
                                 rawPtrGep, "arc_raw_load");
}

void MemoryManager::emitRetain(CodegenContext &ctx, llvm::Value *valPtr,
                               const nlohmann::json &typeJson) {
  if (!isHeapManagedType(typeJson))
    return;
  llvm::Value *rawPtr = extractHeapPointer(ctx, valPtr, typeJson);

  llvm::FunctionCallee retainFn = ctx.Module->getOrInsertFunction(
      rt::RETAIN, llvm::Type::getVoidTy(ctx.Context),
      llvm::PointerType::getUnqual(ctx.Context));
  ctx.Builder->CreateCall(retainFn, {rawPtr});
}

void MemoryManager::emitRelease(CodegenContext &ctx, llvm::Value *valPtr,
                                const nlohmann::json &typeJson) {
  if (!isHeapManagedType(typeJson))
    return;
  llvm::Value *rawPtr = extractHeapPointer(ctx, valPtr, typeJson);

  llvm::FunctionCallee releaseFn = ctx.Module->getOrInsertFunction(
      rt::RELEASE, llvm::Type::getVoidTy(ctx.Context),
      llvm::PointerType::getUnqual(ctx.Context));
  ctx.Builder->CreateCall(releaseFn, {rawPtr});
}

void MemoryManager::trackDeepForRelease(CodegenContext &ctx,
                                        llvm::Value *valPtr,
                                        const nlohmann::json &typeJson) {
  if (typeJson.is_null())
    return;

  if (isRefType(typeJson)) {
    ctx.ScopeStack.back().push_back({valPtr, false, typeJson});
    return;
  }

  std::string_view kind = typeJson["kind"].get<std::string_view>();

  if (kind == "Struct") {
    llvm::Type *structTy = llvmTypeFor(ctx, typeJson);
    for (int i = 0; i < (int)typeJson["fields"].size(); ++i) {
      const auto &field = typeJson["fields"][i];
      if (isRefType(field["type"]) ||
          field["type"]["kind"].get<std::string_view>() == "Struct") {
        llvm::Value *fieldPtr = ctx.Builder->CreateGEP(
            structTy, valPtr,
            {ctx.Builder->getInt32(0), ctx.Builder->getInt32(i)});
        trackDeepForRelease(ctx, fieldPtr, field["type"]);
      }
    }
    return;
  }

  // Array types: only called for heap arrays (see compileArrayLiteral).
  // Track each element slot that itself holds a ref type so popScope can
  // release them individually. Stack arrays must NOT be passed here —
  // their element[0] is data, not a heap pointer.
  if (kind == "Array" && typeJson.contains("base")) {
    const auto &elemType = typeJson["base"];
    if (isRefType(elemType) ||
        (!elemType.is_null() &&
         elemType["kind"].get<std::string_view>() == "Struct")) {
      int size = typeJson["size"].get<int>();
      llvm::Type *arrayTy = llvmTypeFor(ctx, typeJson);
      for (int i = 0; i < size; ++i) {
        llvm::Value *elemPtr = ctx.Builder->CreateGEP(
            arrayTy, valPtr,
            {ctx.Builder->getInt32(0), ctx.Builder->getInt32(i)},
            "array_elem_track");
        trackDeepForRelease(ctx, elemPtr, elemType);
      }
    }
  }
}

void MemoryManager::untrackFromRelease(CodegenContext &ctx,
                                       llvm::Value *valPtr) {
  if (ctx.ScopeStack.empty())
    return;
  auto &currentScope = ctx.ScopeStack.back();

  auto it = std::remove_if(
      currentScope.begin(), currentScope.end(),
      [valPtr](const TrackedItem &item) { return item.ptr == valPtr; });

  if (it != currentScope.end()) {
    currentScope.erase(it, currentScope.end());
  }
}

} // namespace maml