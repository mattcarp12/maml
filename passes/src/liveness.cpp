#include "liveness.h"

namespace maml::passes {

template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

// =============================================================================
// Top-Level Solvers
// =============================================================================

LivenessResult LivenessAnalyzer::analyzeLiveness(
    const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals)
{
    LivenessResult res;
    res.aliases = buildAliasMap(g, locals);

    for (const auto& [id, block] : g->blocks) {
        res.liveIn[id] = {};
        res.liveOut[id] = {};
    }

    std::unordered_map<mir::BlockID, std::unordered_set<SymID>> blockUses;
    std::unordered_map<mir::BlockID, std::unordered_set<SymID>> blockDefs;

    for (const auto& [id, block] : g->blocks) {
        auto [u, d] = computeBlockUseDef(block.get(), res.aliases);
        blockUses[id] = std::move(u);
        blockDefs[id] = std::move(d);
    }

    bool changed = true;
    mir::BlockID maxID = 0;
    for (const auto& [id, block] : g->blocks) {
        if (id > maxID)
            maxID = id;
    }

    // Fixed-point backward dataflow[cite: 3]
    while (changed) {
        changed = false;

        for (mir::BlockID id = maxID; id >= 0; id--) {
            auto it = g->blocks.find(id);
            if (it == g->blocks.end())
                continue;

            const mir::BasicBlock* block = it->second.get();

            // 1. LiveOut = Union of LiveIn of all successors[cite: 3]
            std::vector<mir::BlockID> succs = getSuccessors(block);
            for (mir::BlockID succID : succs) {
                for (SymID v : res.liveIn[succID]) {
                    if (res.liveOut[id].insert(v).second) {
                        changed = true;
                    }
                }
            }

            const auto& useSet = blockUses[id];
            const auto& defSet = blockDefs[id];

            // 2. LiveIn = Use U (LiveOut - Def)[cite: 3]
            for (SymID v : useSet) {
                if (res.liveIn[id].insert(v).second) {
                    changed = true;
                }
            }

            for (SymID v : res.liveOut[id]) {
                if (defSet.find(v) == defSet.end()) {
                    if (res.liveIn[id].insert(v).second) {
                        changed = true;
                    }
                }
            }
        }
    }

    return res;
}

BlockStatementLiveness LivenessAnalyzer::analyzeStatementLiveness(const mir::BasicBlock* block,
    const std::unordered_set<SymID>& blockLiveOut, const std::unordered_map<SymID, SymID>& aliases)
{
    BlockStatementLiveness res;
    size_t numStmts = block->statements.size();
    res.liveIn.resize(numStmts);
    res.liveOut.resize(numStmts);
    res.termLiveOut = blockLiveOut;

    std::unordered_set<SymID> currentLive = blockLiveOut;

    // Process Terminator[cite: 3]
    std::vector<SymID> termUses = getTerminatorUses(block->terminator);
    for (SymID u : termUses) {
        currentLive.insert(u);
        SymID root = resolveAlias(u, aliases);
        if (root != u)
            currentLive.insert(root);
    }
    res.termLiveIn = currentLive;

    // Traverse statements backward[cite: 3]
    for (int i = static_cast<int>(numStmts) - 1; i >= 0; i--) {
        const auto& inst = block->statements[i];
        res.liveOut[i] = currentLive;

        std::vector<SymID> uses, defs;
        getInstUseDef(inst, uses, defs);

        // LiveIn = (LiveOut - Defs) U Uses[cite: 3]
        for (SymID d : defs) {
            currentLive.erase(d);
        }

        for (SymID u : uses) {
            currentLive.insert(u);
            SymID root = resolveAlias(u, aliases);
            if (root != u)
                currentLive.insert(root);
        }

        res.liveIn[i] = currentLive;
    }

    return res;
}

// =============================================================================
// Block-Level Extractors
// =============================================================================

std::pair<std::unordered_set<SymID>, std::unordered_set<SymID>>
LivenessAnalyzer::computeBlockUseDef(
    const mir::BasicBlock* block, const std::unordered_map<SymID, SymID>& aliases)
{
    std::unordered_set<SymID> useSet;
    std::unordered_set<SymID> defSet;

    auto addUse = [&](SymID name) {
        if (name == NoSymbol)
            return;
        useSet.insert(name);
        SymID root = resolveAlias(name, aliases);
        if (root != name)
            useSet.insert(root);
    };

    std::vector<SymID> termUses = getTerminatorUses(block->terminator);
    for (SymID u : termUses)
        addUse(u);

    for (int i = static_cast<int>(block->statements.size()) - 1; i >= 0; i--) {
        std::vector<SymID> uses, defs;
        getInstUseDef(block->statements[i], uses, defs);

        for (SymID d : defs) {
            if (d == NoSymbol)
                continue;
            defSet.insert(d);
            useSet.erase(d);
        }

        for (SymID u : uses)
            addUse(u);
    }

    return { useSet, defSet };
}

// =============================================================================
// Instruction Extractors
// =============================================================================

std::vector<mir::BlockID> LivenessAnalyzer::getSuccessors(const mir::BasicBlock* block)
{
    std::vector<mir::BlockID> succs;
    std::visit(overloaded {
                   [&](std::monostate) {},
                   [&](const mir::JumpTerminator& t) { succs.push_back(t.target); },
                   [&](const mir::BranchTerminator& t) {
                       succs.push_back(t.trueTarget);
                       succs.push_back(t.falseTarget);
                   },
                   [&](const mir::CoroSuspendTerminator& t) {
                       succs.push_back(t.resumeBlock);
                       succs.push_back(t.cleanupBlock);
                   },
                   [&](auto&) {} // Others have no intra-function successors
               },
        block->terminator);
    return succs;
}

std::vector<SymID> LivenessAnalyzer::getTerminatorUses(const mir::Terminator& term)
{
    std::vector<SymID> uses;
    auto addUse = [&](const mir::Value& val) {
        if (auto* reg = std::get_if<mir::Register>(&val))
            uses.push_back(reg->name);
    };

    std::visit(
        overloaded { [&](std::monostate) {},
            [&](const mir::BranchTerminator& t) { addUse(t.condition); },
            [&](const mir::ReturnTerminator& t) { addUse(t.value); },
            [&](const mir::CoroFinalSuspendTerminator& t) { addUse(t.value); }, [&](auto&) {} },
        term);
    return uses;
}

void LivenessAnalyzer::getInstUseDef(
    const mir::Instruction& inst, std::vector<SymID>& uses, std::vector<SymID>& defs)
{
    auto addUseVal = [&](const mir::Value& val) {
        if (auto* reg = std::get_if<mir::Register>(&val)) {
            if (reg->name != NoSymbol)
                uses.push_back(reg->name);
        }
    };
    auto addUseName = [&](SymID name) {
        if (name != NoSymbol)
            uses.push_back(name);
    };
    auto addDef = [&](SymID name) {
        if (name != NoSymbol)
            defs.push_back(name);
    };

    std::visit(
        overloaded { [&](std::monostate) {}, [&](const mir::AllocaInst& i) { addDef(i.dst); },
            [&](const mir::AssignInst& i) {
                addDef(i.dst);
                addUseVal(i.rValue);
            },
            [&](const mir::CopyInst& i) {
                addDef(i.dst);
                addUseName(i.src);
            },
            [&](const mir::MoveInst& i) {
                addDef(i.dst);
                addUseName(i.src);
            },
            [&](const mir::BorrowInst& i) {
                addDef(i.dst);
                addUseName(i.src);
            },
            [&](const mir::CastInst& i) {
                addDef(i.dst);
                addUseVal(i.src);
            },
            [&](const mir::BinaryOpInst& i) {
                addDef(i.dst);
                addUseVal(i.left);
                addUseVal(i.right);
            },
            [&](const mir::UnaryOpInst& i) {
                addDef(i.dst);
                addUseVal(i.operand);
            },
            [&](const mir::BitcastPtrInst& i) {
                addDef(i.dst);
                addUseVal(i.src);
            },
            [&](const mir::FieldAddrInst& i) {
                addDef(i.dst);
                addUseVal(i.object);
            },
            [&](const mir::LoadPtrInst& i) {
                addDef(i.dst);
                addUseVal(i.ptr);
            },
            [&](const mir::StoreInst& i) {
                addUseVal(i.dstPtr);
                addUseVal(i.value);
            },
            [&](const mir::IndexAddrInst& i) {
                addDef(i.dst);
                addUseVal(i.source);
                addUseVal(i.index);
            },
            [&](const mir::CallInst& i) {
                if (i.dst != NoSymbol)
                    addDef(i.dst);
                addUseVal(i.function);
                for (const auto& arg : i.arguments)
                    addUseVal(arg);
            },
            [&](const mir::KeepAliveInst& i) { addUseName(i.src); }, [&](auto&) {} },
        inst);
}

// =============================================================================
// Alias Graph Tracking
// =============================================================================

std::unordered_map<SymID, SymID> LivenessAnalyzer::buildAliasMap(
    const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals)
{
    std::unordered_map<SymID, SymID> aliases;

    auto isView = [&](SymID name) {
        auto it = locals.find(name);
        if (it == locals.end() || it->second == nullptr)
            return false;
        return it->second->kind == types::TypeKind::View;
    };

    for (const auto& [id, block] : g->blocks) {
        for (const auto& inst : block->statements) {
            std::visit(
                overloaded { [&](std::monostate) {},
                    [&](const mir::BorrowInst& i) { aliases[i.dst] = i.src; },
                    [&](const mir::AddressOfInst& i) { aliases[i.dst] = i.src; },
                    [&](const mir::FieldAddrInst& i) {
                        if (auto* reg = std::get_if<mir::Register>(&i.object))
                            aliases[i.dst] = reg->name;
                    },
                    [&](const mir::IndexAddrInst& i) {
                        if (auto* reg = std::get_if<mir::Register>(&i.source))
                            aliases[i.dst] = reg->name;
                    },
                    [&](const mir::LoadPtrInst& i) {
                        if (auto* reg = std::get_if<mir::Register>(&i.ptr)) {
                            SymID root = resolveAlias(reg->name, aliases);
                            if (root != reg->name)
                                aliases[i.dst] = root;
                        }
                    },
                    [&](const mir::BitcastPtrInst& i) {
                        if (auto* reg = std::get_if<mir::Register>(&i.src))
                            aliases[i.dst] = reg->name;
                    },
                    [&](const mir::CallInst& i) {
                        if (auto* reg = std::get_if<mir::Register>(&i.function)) {
                            std::string_view name = sym_.resolve(reg->name);
                            if ((name == "maml_vec_get" || name == "maml_map_get")
                                && !i.arguments.empty()) {
                                if (auto* argReg = std::get_if<mir::Register>(&i.arguments[0])) {
                                    aliases[i.dst] = argReg->name;
                                }
                            }
                        }
                    },
                    [&](const mir::CoroPromisePtrInst& i) {
                        if (auto* reg = std::get_if<mir::Register>(&i.handle))
                            aliases[i.dst] = reg->name;
                    },
                    [&](const mir::StoreInst& i) {
                        if (auto* srcReg = std::get_if<mir::Register>(&i.value)) {
                            if (auto* dstReg = std::get_if<mir::Register>(&i.dstPtr)) {
                                SymID dstRoot = resolveAlias(dstReg->name, aliases);
                                if (isView(dstRoot)) {
                                    SymID srcRoot = resolveAlias(srcReg->name, aliases);
                                    if (srcRoot != dstRoot)
                                        aliases[dstRoot] = srcRoot;
                                }
                            }
                        }
                    },
                    // Track alias transfers for Views[cite: 3]
                    [&](const mir::MoveInst& i) {
                        if (isView(i.dst)) {
                            SymID root = resolveAlias(i.src, aliases);
                            if (root != i.src)
                                aliases[i.dst] = root;
                        }
                    },
                    [&](const mir::CopyInst& i) {
                        if (isView(i.dst)) {
                            SymID root = resolveAlias(i.src, aliases);
                            if (root != i.src)
                                aliases[i.dst] = root;
                        }
                    },
                    [&](const mir::AssignInst& i) {
                        if (isView(i.dst)) {
                            if (auto* reg = std::get_if<mir::Register>(&i.rValue)) {
                                SymID root = resolveAlias(reg->name, aliases);
                                if (root != reg->name)
                                    aliases[i.dst] = root;
                            }
                        }
                    },
                    [&](auto&) {} },
                inst);
        }
    }
    return aliases;
}

SymID LivenessAnalyzer::resolveAlias(SymID name, const std::unordered_map<SymID, SymID>& aliases)
{
    std::unordered_set<SymID> visited;
    SymID current = name;

    while (true) {
        visited.insert(current);
        auto it = aliases.find(current);

        // Found root[cite: 3]
        if (it == aliases.end() || it->second == current) {
            return current;
        }

        // Cycle detected, break safely[cite: 3]
        if (visited.find(it->second) != visited.end()) {
            return current;
        }

        current = it->second;
    }
}

} // namespace maml::passes