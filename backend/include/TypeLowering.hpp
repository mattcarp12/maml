#pragma once

#include "CodegenContext.hpp"
#include "types.h"
#include <llvm/IR/Type.h>

namespace maml {

// Converts a MAML semantic type into an LLVM IR type.
llvm::Type* llvmTypeFor(CodegenContext& ctx, const types::Type* type);

// Retrieves the raw structural layout of a type, bypassing opaque pointers for runtime composites.
llvm::Type* llvmLayoutTypeFor(CodegenContext& ctx, const types::Type* type);

} // namespace maml