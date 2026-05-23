#ifndef MAML_TYPE_LOWERING_H
#define MAML_TYPE_LOWERING_H

#include "CodegenContext.h"
#include <llvm/IR/Type.h>
#include <nlohmann/json.hpp>

namespace maml {

// Translates a MAML JSON type definition into a native LLVM IR Type
llvm::Type *llvmTypeFor(CodegenContext &ctx, const nlohmann::json &typeJson);

} // namespace maml

#endif // MAML_TYPE_LOWERING_H