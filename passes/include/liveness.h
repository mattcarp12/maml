#pragma once

#include "cfg.h"
#include "mir.h"
#include "sym.h"
#include "types.h"
#include <unordered_map>
#include <unordered_set>
#include <vector>

namespace maml::passes {

struct LivenessResult {
    std::unordered_map<mir::BlockID, std::unordered_set<SymID>> liveIn;
    std::unordered_map<mir::BlockID, std::unordered_set<SymID>> liveOut;
    std::unordered_map<SymID, SymID> aliases;
};

struct BlockStatementLiveness {
    std::vector<std::unordered_set<SymID>> liveIn;
    std::vector<std::unordered_set<SymID>> liveOut;
    std::unordered_set<SymID> termLiveIn;
    std::unordered_set<SymID> termLiveOut;
};

class LivenessAnalyzer {
public:
    LivenessAnalyzer(SymbolTable& sym)
        : sym_(sym)
    {
    }

    LivenessResult analyzeLiveness(
        const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals);
    BlockStatementLiveness analyzeStatementLiveness(const mir::BasicBlock* block,
        const std::unordered_set<SymID>& blockLiveOut,
        const std::unordered_map<SymID, SymID>& aliases);

private:
    SymbolTable& sym_;

    std::unordered_map<SymID, SymID> buildAliasMap(
        const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals);
    SymID resolveAlias(SymID name, const std::unordered_map<SymID, SymID>& aliases);

    std::pair<std::unordered_set<SymID>, std::unordered_set<SymID>> computeBlockUseDef(
        const mir::BasicBlock* block, const std::unordered_map<SymID, SymID>& aliases);

    std::vector<SymID> getTerminatorUses(const mir::Terminator& term);
    void getInstUseDef(
        const mir::Instruction& inst, std::vector<SymID>& uses, std::vector<SymID>& defs);
    std::vector<mir::BlockID> getSuccessors(const mir::BasicBlock* block);
};

} // namespace maml::passes