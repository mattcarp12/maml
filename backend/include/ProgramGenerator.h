#ifndef MAML_PROGRAM_GENERATOR_H
#define MAML_PROGRAM_GENERATOR_H

#include <llvm/IR/Function.h>

#include <nlohmann/json.hpp>

#include "CodegenContext.h"

namespace maml {

void compileFunction(CodegenContext &ctx, const nlohmann::json &fn);
void compileProgram(CodegenContext &ctx, const nlohmann::json &ast);

}  // namespace maml

#endif  // MAML_PROGRAM_GENERATOR_H