#pragma once
#include "CodegenContext.hpp"
#include "mir.h"

namespace maml {

void compileInstruction(CodegenContext& ctx, const mir::Instruction& inst);
void compileTerminator(CodegenContext& ctx, const mir::Terminator& term);

// Helper to check if a Value operand was omitted (unset)
inline bool isEmpty(const mir::Value& v)
{
    if (auto* reg = std::get_if<mir::Register>(&v)) {
        return reg->name == NoSymbol;
    }
    return false;
}

} // namespace maml