#ifndef MAML_STMT_GENERATOR_H
#define MAML_STMT_GENERATOR_H

#include "CodegenContext.h"
#include <nlohmann/json.hpp>

namespace maml {

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt);

} // namespace maml

#endif // MAML_STMT_GENERATOR_H