#pragma once

#include <llvm/IR/Type.h>

#include <memory>

#include "CodegenContext.hpp"
#include "types_generated.hpp"

namespace maml {
llvm::Type* llvmTypeFor(CodegenContext& ctx, const std::shared_ptr<maml::Type>& type);
llvm::Type* llvmLayoutTypeFor(CodegenContext& ctx, const std::shared_ptr<maml::Type>& type);
}  // namespace maml