#ifndef MAML_PROGRAM_GENERATOR_H
#define MAML_PROGRAM_GENERATOR_H

#include "CodegenContext.h"
#include "mir/mir_generated.hpp"

namespace maml {
void compileFunction(CodegenContext &ctx, const mir::Function &fn);
void compileProgram(CodegenContext &ctx, const mir::Program &prog);
}  // namespace maml

#endif