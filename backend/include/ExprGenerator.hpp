#pragma once

#include <llvm/IR/Value.h>

#include "CodegenContext.hpp"
#include "mir_generated.hpp"

namespace maml {
llvm::Value *evaluateValue(CodegenContext &ctx, const mir::Value &val);
}
