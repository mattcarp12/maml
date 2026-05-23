#ifndef MAML_MEMORY_MANAGER_H
#define MAML_MEMORY_MANAGER_H

#include "CodegenContext.h"
#include <llvm/IR/Value.h>
#include <nlohmann/json.hpp>

namespace maml {

class MemoryManager {
public:
  static bool isHeapManagedType(const nlohmann::json &typeJson);
  static bool isRefType(const nlohmann::json &typeJson);
  static bool needsARC(const nlohmann::json &typeJson);

  static llvm::Value *extractHeapPointer(CodegenContext &ctx,
                                         llvm::Value *containerPtr,
                                         const nlohmann::json &typeJson);

  static void emitRetain(CodegenContext &ctx, llvm::Value *valPtr,
                         const nlohmann::json &typeJson);
  static void emitRelease(CodegenContext &ctx, llvm::Value *valPtr,
                          const nlohmann::json &typeJson);
  static void trackDeepForRelease(CodegenContext &ctx, llvm::Value *valPtr,
                                  const nlohmann::json &typeJson);
  static void untrackFromRelease(CodegenContext &ctx, llvm::Value *valPtr);
};

} // namespace maml

#endif // MAML_MEMORY_MANAGER_H