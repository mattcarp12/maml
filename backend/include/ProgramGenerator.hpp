#pragma once

#include "CodegenContext.hpp"
#include "cfg.h"

namespace maml {
void compileFunction(CodegenContext& ctx, const mir::Function& fn);
void compileProgram(CodegenContext& ctx, const mir::Program& prog);
} // namespace maml