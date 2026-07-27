#pragma once

#include "CodegenContext.hpp"
#include "mir.h"
#include <llvm/IR/Value.h>

namespace maml {

llvm::Value* evaluateValue(CodegenContext& ctx, const mir::Value& val);
llvm::Value* evaluateAddress(CodegenContext& ctx, const mir::Value& val);

} // namespace maml