#pragma once

#include "CodegenContext.hpp"
#include "mir_generated.hpp"

namespace maml {
void compileFunction(CodegenContext &ctx, const mir::Function &fn);
void compileProgram(CodegenContext &ctx, const mir::Program &prog);
}  // namespace maml
