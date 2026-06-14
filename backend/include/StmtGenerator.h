#ifndef MAML_STMT_GENERATOR_H
#define MAML_STMT_GENERATOR_H

#include "CodegenContext.h"
#include "mir/mir_generated.hpp"

namespace maml {

void compileInstruction(CodegenContext &ctx, const mir::Instruction &inst);
void compileTerminator(CodegenContext &ctx, const mir::Terminator &term);

// Helper to check if a Value operand was omitted (null)
inline bool isEmpty(const mir::Value &v) {
  if (auto *reg = std::get_if<mir::Register>(&v.inner)) {
    return reg->name.empty();
  }
  return false;
}

}  // namespace maml

#endif