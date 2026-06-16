#pragma once

#include <llvm/IR/Type.h>

#include <memory>

#include "CodegenContext.hpp"
#include "types_generated.hpp"

namespace maml {

// Strongly-typed representation of compound MIR type kinds
enum class TypeKind { Unknown, Struct, SumType, Array, Vector, View, Map, Future };

// Translates a strongly-typed MAML AST/MIR Type into a native LLVM IR Type
llvm::Type *llvmTypeFor(CodegenContext &ctx, const std::shared_ptr<maml::Type> &type);

// Note: getTypeKind has been removed as we will now rely on C++ std::holds_alternative

}  // namespace maml