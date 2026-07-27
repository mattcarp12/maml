#include "linear_lower.h"

namespace maml::passes {

void lowerLinearTypes(mir::Graph* g)
{
    if (!g)
        return;

    for (auto& [_, block] : g->blocks) {
        std::vector<mir::Instruction> newStmts;
        for (auto& inst : block->statements) {
            if (auto* borrow = std::get_if<mir::BorrowInst>(&inst)) {
                newStmts.push_back(mir::AddressOfInst { borrow->dst, borrow->src, borrow->pos });
            } else if (std::holds_alternative<mir::KeepAliveInst>(inst)) {
                continue; // Strip KeepAlive instructions[cite: 3]
            } else {
                newStmts.push_back(std::move(inst));
            }
        }
        block->statements = std::move(newStmts);
    }
}

} // namespace maml::passes