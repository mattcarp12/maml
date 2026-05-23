#ifndef MAML_PROGRAM_GENERATOR_H
#define MAML_PROGRAM_GENERATOR_H

#include "CodegenContext.h"
#include <llvm/IR/Function.h>
#include <nlohmann/json.hpp>

namespace maml {

void compileCoroutinePrologue(CodegenContext &ctx, llvm::Function *F);
void compileFunction(CodegenContext &ctx, const nlohmann::json &fn);
void compileProgram(CodegenContext &ctx, const nlohmann::json &ast);

} // namespace maml

#endif // MAML_PROGRAM_GENERATOR_H