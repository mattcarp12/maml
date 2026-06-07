#ifndef MAML_TYPE_LOWERING_H
#define MAML_TYPE_LOWERING_H

#include <llvm/IR/Type.h>

#include <nlohmann/json.hpp>

#include "CodegenContext.h"

namespace maml {

// Strongly-typed representation of compound MIR type kinds
enum class TypeKind { Unknown, Struct, SumType, Array, Vector, View, Map };

// Translates a MAML JSON type definition into a native LLVM IR Type
llvm::Type *llvmTypeFor(CodegenContext &ctx, const nlohmann::json &typeJson);

}  // namespace maml

#endif  // MAML_TYPE_LOWERING_H