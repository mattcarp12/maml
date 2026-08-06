#include "reachability.h"
#include "cfg.h"
#include "mir.h"
#include "sym.h"
#include <string>
#include <unordered_set>
#include <utility>
#include <variant>
#include <vector>

namespace maml::passes {

void eliminateDeadFunctions(mir::Program* prog, SymbolTable& sym)
{
    if (!prog)
        return;

    std::unordered_set<SymID> liveSet;
    std::vector<SymID> worklist;

    auto addEntryPoint = [&](const std::string& name) {
        SymID id = sym.intern(name);
        worklist.push_back(id);
        liveSet.insert(id);
    };

    // 1. Mark entry points
    addEntryPoint("main");
    addEntryPoint("maml_coro_runtime_init");
    addEntryPoint("maml_alloc");
    addEntryPoint("maml_free");

    auto getFunc = [&](SymID name) -> mir::Function* {
        for (auto& fn : prog->functions) {
            if (fn.name == name)
                return &fn;
        }
        return nullptr;
    };

    // 2. Trace the call graph
    while (!worklist.empty()) {
        SymID current = worklist.back();
        worklist.pop_back();

        mir::Function* fn = getFunc(current);
        if (!fn || !fn->graph)
            continue; // Might be an extern function with no body[cite: 3]

        // Look for CallInsts in all blocks[cite: 3]
        for (const auto& [_, block] : fn->graph->blocks) {
            for (const auto& inst : block->statements) {
                if (auto* call = std::get_if<mir::CallInst>(&inst)) {
                    // The function being called is stored in the Function Value[cite: 3]
                    if (auto* reg = std::get_if<mir::Register>(&call->function)) {
                        SymID targetName = reg->name;
                        if (liveSet.find(targetName) == liveSet.end()) {
                            liveSet.insert(targetName);
                            worklist.push_back(targetName);
                        }
                    }
                }
            }
        }
    }

    // 3. Prune the dead functions
    std::vector<mir::Function> liveFunctions;
    for (auto& fn : prog->functions) {
        if (liveSet.find(fn.name) != liveSet.end()) {
            liveFunctions.push_back(std::move(fn));
        }
    }
    prog->functions = std::move(liveFunctions);
}

} // namespace maml::passes