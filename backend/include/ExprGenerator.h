#ifndef MAML_EXPR_GENERATOR_H
#define MAML_EXPR_GENERATOR_H

#include <llvm/IR/Value.h>

#include "CodegenContext.h"
#include "mir/mir_generated.hpp"

namespace maml {
llvm::Value *evaluateValue(CodegenContext &ctx, const mir::Value &val);
}

#endif