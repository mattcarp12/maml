#include "borrow.h"
#include "mir.h"
#include <algorithm>
#include <format>
#include <stdexcept>

namespace maml::passes {

template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

// =============================================================================
// State Definitions & Helpers
// =============================================================================

std::string toString(LockState s)
{
    switch (s) {
    case LockState::ExclusiveWrite:
        return "ExclusiveWrite";
    case LockState::SharedRead:
        return "SharedRead";
    case LockState::MaybeInvalidated:
        return "MaybeInvalidated";
    case LockState::Invalidated:
        return "Invalidated";
    default:
        return "Unknown";
    }
}

LockState joinStates(LockState s1, LockState s2)
{
    if (s1 == s2)
        return s1;
    if (s1 == LockState::Invalidated || s2 == LockState::Invalidated)
        return LockState::MaybeInvalidated;
    if (s1 == LockState::MaybeInvalidated || s2 == LockState::MaybeInvalidated)
        return LockState::MaybeInvalidated;
    if (s1 == LockState::SharedRead || s2 == LockState::SharedRead)
        return LockState::SharedRead;
    return LockState::ExclusiveWrite;
}

LockState BindingState::aggregateState() const
{
    LockState effective = state;
    for (const auto& [_, field] : fields) {
        LockState fieldState = field->aggregateState();
        if (fieldState == LockState::Invalidated || fieldState == LockState::MaybeInvalidated) {
            if (effective == LockState::ExclusiveWrite || effective == LockState::SharedRead) {
                effective = LockState::MaybeInvalidated;
            }
        }
    }
    return effective;
}

std::unique_ptr<BindingState> BindingState::clone() const
{
    auto c = std::make_unique<BindingState>();
    c->state = state;
    c->mutLockedBy = mutLockedBy;
    c->dependsOn = dependsOn;
    c->aliasOf = aliasOf;
    for (const auto& [k, v] : fields) {
        c->fields[k] = v->clone();
    }
    return c;
}

std::unique_ptr<BlockState> BlockState::clone() const
{
    auto c = std::make_unique<BlockState>();
    for (const auto& [k, v] : bindings) {
        c->bindings[k] = v ? v->clone() : nullptr;
    }
    return c;
}

// =============================================================================
// Dataflow Analyzer
// =============================================================================

bool BorrowAnalyzer::isRef(SymID name) const
{
    auto it = varTypes_.find(name);
    if (it == varTypes_.end() || it->second == nullptr)
        return false;
    return it->second->kind == types::TypeKind::Ptr || it->second->kind == types::TypeKind::View;
}

bool BorrowAnalyzer::isCompilerGenerated(SymID name) const
{
    std::string_view str = sym_.resolve(name);
    return str.starts_with("__") || str.starts_with("_t");
}

void BorrowAnalyzer::errorf(Position pos, const std::string& msg)
{
    if (!reportErrors_)
        return;
    errors_.push_back(ast::CompileError { "Ownership", pos, msg });
}

std::vector<ast::CompileError> BorrowAnalyzer::analyze(const mir::Graph* g,
    const std::unordered_map<SymID, const types::Type*>& locals, const LivenessResult& live)
{
    for (const auto& p : g->params)
        varTypes_[p.name] = p.type;
    for (const auto& [name, t] : locals)
        varTypes_[name] = t;

    std::unordered_map<mir::BlockID, std::unique_ptr<BlockState>> stateIn;
    std::unordered_map<mir::BlockID, std::unique_ptr<BlockState>> stateOut;
    std::unordered_map<mir::BlockID, bool> visited;

    for (const auto& [id, _] : g->blocks) {
        stateIn[id] = std::make_unique<BlockState>();
        stateOut[id] = std::make_unique<BlockState>();
    }

    std::vector<mir::BlockID> worklist;
    std::unordered_map<mir::BlockID, bool> inWorklist;

    for (mir::BasicBlock* block : g->sortedBlocks()) {
        worklist.push_back(block->id);
        inWorklist[block->id] = true;
    }

    // --- PASS 1: fixed-point convergence, no diagnostics ---
    reportErrors_ = false;
    int maxIters = 10000;
    int iters = 0;

    LivenessAnalyzer livenessAnalyzer(sym_);

    while (!worklist.empty()) {
        if (iters > maxIters)
            throw std::runtime_error("borrow checker fixed-point solver failed to converge");
        iters++;

        mir::BlockID id = worklist.front();
        worklist.erase(worklist.begin());
        inWorklist[id] = false;

        const mir::BasicBlock* block = g->blocks.at(id).get();
        std::unique_ptr<BlockState> mergedIn = mergePredecessors(g, block, stateOut, visited);
        stateIn[block->id] = mergedIn->clone();
        std::unique_ptr<BlockState> currentState = mergedIn->clone();

        BlockStatementLiveness stmtLiveness = livenessAnalyzer.analyzeStatementLiveness(
            block, live.liveOut.at(block->id), live.aliases);
        runBlock(block, currentState.get(), stmtLiveness, live);

        visited[block->id] = true;

        if (!statesEqual(stateOut[block->id].get(), currentState.get())) {
            stateOut[block->id] = std::move(currentState);
            for (mir::BlockID succID : getSuccessors(block)) {
                if (!inWorklist[succID]) {
                    worklist.push_back(succID);
                    inWorklist[succID] = true;
                }
            }
        }
    }

    // --- PASS 2: converged state, diagnostics enabled ---
    reportErrors_ = true;

    for (const mir::BasicBlock* block : g->sortedBlocks()) {
        std::unique_ptr<BlockState> currentState = stateIn[block->id]->clone();
        BlockStatementLiveness stmtLiveness = livenessAnalyzer.analyzeStatementLiveness(
            block, live.liveOut.at(block->id), live.aliases);
        runBlock(block, currentState.get(), stmtLiveness, live);
    }

    validateScopeExit(g, stateOut);
    return errors_;
}

void BorrowAnalyzer::runBlock(const mir::BasicBlock* block, BlockState* currentState,
    const BlockStatementLiveness& stmtLiveness, const LivenessResult& live)
{
    for (size_t i = 0; i < block->statements.size(); ++i) {
        analyzeInstruction(block->statements[i], currentState);
        for (auto& [_, binding] : currentState->bindings) {
            if (binding && binding->mutLockedBy != NoSymbol) {
                if (stmtLiveness.liveOut[i].find(binding->mutLockedBy)
                    == stmtLiveness.liveOut[i].end()) {
                    binding->mutLockedBy = NoSymbol;
                }
            }
        }
    }

    const auto& blockLiveOut = live.liveOut.at(block->id);
    analyzeTerminator(block->terminator, currentState, blockLiveOut);

    for (auto& [_, binding] : currentState->bindings) {
        if (binding && binding->mutLockedBy != NoSymbol) {
            if (blockLiveOut.find(binding->mutLockedBy) == blockLiveOut.end()) {
                binding->mutLockedBy = NoSymbol;
            }
        }
    }

    auto invalidateDeadProvenance = [&](auto& self, BindingState* b) -> void {
        if (!b)
            return;
        if (b->dependsOn != NoSymbol) {
            if (blockLiveOut.find(b->dependsOn) == blockLiveOut.end()) {
                b->state = LockState::Invalidated;
            }
        }
        for (auto& [_, f] : b->fields) {
            self(self, f.get());
        }
    };

    for (auto& [_, binding] : currentState->bindings) {
        invalidateDeadProvenance(invalidateDeadProvenance, binding.get());
    }
}

std::unique_ptr<BlockState> BorrowAnalyzer::mergePredecessors(const mir::Graph* g,
    const mir::BasicBlock* block,
    const std::unordered_map<mir::BlockID, std::unique_ptr<BlockState>>& stateOut,
    const std::unordered_map<mir::BlockID, bool>& visited)
{
    auto merged = std::make_unique<BlockState>();
    std::vector<mir::BlockID> allPreds = getPredecessors(g, block->id);
    std::vector<mir::BlockID> preds;

    for (mir::BlockID p : allPreds) {
        auto it = visited.find(p);
        if (it != visited.end() && it->second) {
            preds.push_back(p);
        }
    }
    if (preds.empty())
        return merged;

    for (mir::BlockID p : preds) {
        for (const auto& [k, _] : stateOut.at(p)->bindings) {
            if (merged->bindings.find(k) == merged->bindings.end()) {
                merged->bindings[k] = nullptr;
            }
        }
    }

    for (auto& [k, _] : merged->bindings) {
        std::unique_ptr<BindingState> currentMerged = nullptr;
        for (mir::BlockID p : preds) {
            const BindingState* predVal = nullptr;
            auto it = stateOut.at(p)->bindings.find(k);
            if (it != stateOut.at(p)->bindings.end()) {
                predVal = it->second.get();
            }
            if (!currentMerged) {
                if (predVal)
                    currentMerged = predVal->clone();
                else {
                    currentMerged = std::make_unique<BindingState>();
                    currentMerged->state = LockState::Invalidated;
                }
            } else {
                // Because predVal is now a raw pointer, pass it directly
                currentMerged = mergeBindings(currentMerged.get(), predVal);
            }
        }
        merged->bindings[k] = std::move(currentMerged);
    }
    return merged;
}

std::unique_ptr<BindingState> BorrowAnalyzer::mergeBindings(
    const BindingState* b1, const BindingState* b2)
{
    if (!b1 && !b2)
        return nullptr;
    if (!b1)
        return b2->clone();
    if (!b2)
        return b1->clone();

    auto res = std::make_unique<BindingState>();
    res->state = joinStates(b1->state, b2->state);

    if (b1->mutLockedBy != NoSymbol)
        res->mutLockedBy = b1->mutLockedBy;
    else if (b2->mutLockedBy != NoSymbol)
        res->mutLockedBy = b2->mutLockedBy;

    if (b1->dependsOn != NoSymbol)
        res->dependsOn = b1->dependsOn;
    else if (b2->dependsOn != NoSymbol)
        res->dependsOn = b2->dependsOn;

    if (b1->aliasOf != NoSymbol)
        res->aliasOf = b1->aliasOf;
    else if (b2->aliasOf != NoSymbol)
        res->aliasOf = b2->aliasOf;

    std::unordered_set<SymID> allKeys;
    for (const auto& [k, _] : b1->fields)
        allKeys.insert(k);
    for (const auto& [k, _] : b2->fields)
        allKeys.insert(k);

    for (SymID k : allKeys) {
        const BindingState* f1 = b1->fields.count(k) ? b1->fields.at(k).get() : nullptr;
        const BindingState* f2 = b2->fields.count(k) ? b2->fields.at(k).get() : nullptr;

        std::unique_ptr<BindingState> tmp1, tmp2;
        if (!f1) {
            tmp1 = std::make_unique<BindingState>();
            tmp1->state = b1->state;
            f1 = tmp1.get();
        }
        if (!f2) {
            tmp2 = std::make_unique<BindingState>();
            tmp2->state = b2->state;
            f2 = tmp2.get();
        }

        res->fields[k] = mergeBindings(f1, f2);
    }
    return res;
}

std::vector<mir::BlockID> BorrowAnalyzer::getPredecessors(
    const mir::Graph* g, mir::BlockID target) const
{
    std::vector<mir::BlockID> preds;
    for (const mir::BasicBlock* block : g->sortedBlocks()) {
        std::visit(overloaded { [&](std::monostate) {},
                       [&](const mir::JumpTerminator& t) {
                           if (t.target == target)
                               preds.push_back(block->id);
                       },
                       [&](const mir::BranchTerminator& t) {
                           if (t.trueTarget == target || t.falseTarget == target)
                               preds.push_back(block->id);
                       },
                       [&](auto&) {} },
            block->terminator);
    }
    return preds;
}

std::vector<mir::BlockID> BorrowAnalyzer::getSuccessors(const mir::BasicBlock* block) const
{
    std::vector<mir::BlockID> succs;
    std::visit(overloaded { [&](std::monostate) {},
                   [&](const mir::JumpTerminator& t) { succs.push_back(t.target); },
                   [&](const mir::BranchTerminator& t) {
                       succs.push_back(t.trueTarget);
                       succs.push_back(t.falseTarget);
                   },
                   [&](const mir::CoroSuspendTerminator& t) {
                       succs.push_back(t.resumeBlock);
                       succs.push_back(t.cleanupBlock);
                   },
                   [&](auto&) {} },
        block->terminator);
    return succs;
}

bool BorrowAnalyzer::statesEqual(const BlockState* s1, const BlockState* s2) const
{
    if (s1->bindings.size() != s2->bindings.size())
        return false;
    for (const auto& [k, v1] : s1->bindings) {
        auto it = s2->bindings.find(k);
        if (it == s2->bindings.end() || !bindingsEqual(v1.get(), it->second.get()))
            return false;
    }
    return true;
}

bool BorrowAnalyzer::bindingsEqual(const BindingState* b1, const BindingState* b2) const
{
    if (!b1 && !b2)
        return true;
    if (!b1 || !b2)
        return false;
    if (b1->state != b2->state || b1->mutLockedBy != b2->mutLockedBy)
        return false;
    if (b1->dependsOn != b2->dependsOn || b1->aliasOf != b2->aliasOf)
        return false;
    if (b1->fields.size() != b2->fields.size())
        return false;
    for (const auto& [k, f1] : b1->fields) {
        auto it = b2->fields.find(k);
        if (it == b2->fields.end() || !bindingsEqual(f1.get(), it->second.get()))
            return false;
    }
    return true;
}

// =============================================================================
// Instruction Transfer Functions
// =============================================================================

void BorrowAnalyzer::analyzeInstruction(const mir::Instruction& inst, BlockState* state)
{
    Position pos = mir::getPosOf(inst);

    auto releaseLocksHeldBy = [&](SymID refName) {
        for (auto& [_, binding] : state->bindings) {
            if (binding && binding->mutLockedBy == refName)
                binding->mutLockedBy = NoSymbol;
        }
    };

    auto initOrRevive = [&](SymID name) {
        if (state->bindings.count(name) && state->bindings[name]) {
            state->bindings[name]->state = LockState::ExclusiveWrite;
            state->bindings[name]->fields.clear();
            state->bindings[name]->aliasOf = NoSymbol;
            state->bindings[name]->dependsOn = NoSymbol;
        } else {
            auto b = std::make_unique<BindingState>();
            b->state = LockState::ExclusiveWrite;
            state->bindings[name] = std::move(b);
        }
    };

    std::visit(
        overloaded { [&](std::monostate) {}, [&](const mir::AllocaInst& i) { initOrRevive(i.dst); },
            [&](const mir::AssignInst& i) {
                checkOperandAccess(i.rValue, state, pos);
                releaseLocksHeldBy(i.dst);
                if (auto* reg = std::get_if<mir::Register>(&i.rValue)) {
                    if (state->bindings.count(reg->name) && state->bindings[reg->name]) {
                        auto newBinding = state->bindings[reg->name]->clone();
                        if (isRef(reg->name) && !isCompilerGenerated(reg->name)) {
                            newBinding->aliasOf = reg->name;
                            newBinding->state
                                = joinStates(newBinding->aggregateState(), LockState::SharedRead);
                            state->bindings[reg->name]->state
                                = joinStates(state->bindings[reg->name]->aggregateState(),
                                    LockState::SharedRead);
                        } else {
                            newBinding->aliasOf = NoSymbol;
                        }
                        state->bindings[i.dst] = std::move(newBinding);
                    } else
                        initOrRevive(i.dst);
                } else
                    initOrRevive(i.dst);
            },
            [&](const mir::CopyInst& i) {
                checkStringAccess(i.src, state, pos);
                releaseLocksHeldBy(i.dst);
                if (state->bindings.count(i.src) && state->bindings[i.src]) {
                    auto newBinding = state->bindings[i.src]->clone();
                    if (isRef(i.src) && !isCompilerGenerated(i.src)) {
                        newBinding->aliasOf = i.src;
                        newBinding->state
                            = joinStates(newBinding->aggregateState(), LockState::SharedRead);
                        state->bindings[i.src]->state = joinStates(
                            state->bindings[i.src]->aggregateState(), LockState::SharedRead);
                    } else {
                        newBinding->aliasOf = NoSymbol;
                    }
                    state->bindings[i.dst] = std::move(newBinding);
                } else
                    initOrRevive(i.dst);
            },
            [&](const mir::MoveInst& i) {
                checkStringAccess(i.src, state, pos);
                releaseLocksHeldBy(i.dst);
                if (state->bindings.count(i.src) && state->bindings[i.src]) {
                    auto newBinding = state->bindings[i.src]->clone();
                    if (isRef(i.src) && !isCompilerGenerated(i.src)) {
                        newBinding->aliasOf = i.src;
                        newBinding->state
                            = joinStates(newBinding->aggregateState(), LockState::SharedRead);
                        state->bindings[i.src]->state = joinStates(
                            state->bindings[i.src]->aggregateState(), LockState::SharedRead);
                    } else {
                        newBinding->aliasOf = NoSymbol;
                        state->bindings[i.src]->state = LockState::Invalidated;
                    }
                    state->bindings[i.dst] = std::move(newBinding);
                } else
                    initOrRevive(i.dst);
            },
            [&](const mir::BorrowInst& i) {
                checkStringAccess(i.src, state, pos);
                releaseLocksHeldBy(i.dst);
                if (state->bindings.count(i.src) && state->bindings[i.src]) {
                    if (i.isMut) {
                        if (state->bindings[i.src]->aliasOf != NoSymbol) {
                            errorf(pos,
                                std::format("cannot take mutable borrow of aliased data '{}'",
                                    sym_.resolve(i.src)));
                        } else if (state->bindings[i.src]->aggregateState()
                            != LockState::ExclusiveWrite) {
                            errorf(pos,
                                std::format("cannot borrow '{}' mutably; current state is {}",
                                    sym_.resolve(i.src),
                                    toString(state->bindings[i.src]->aggregateState())));
                        }
                        state->bindings[i.src]->mutLockedBy = i.dst;
                    } else {
                        state->bindings[i.src]->state = joinStates(
                            state->bindings[i.src]->aggregateState(), LockState::SharedRead);
                    }
                }
                initOrRevive(i.dst);
                state->bindings[i.dst]->dependsOn = i.src;
            },
            [&](const mir::BitcastPtrInst& i) {
                checkOperandAccess(i.src, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
                if (auto* reg = std::get_if<mir::Register>(&i.src)) {
                    if (state->bindings.count(reg->name) && state->bindings[reg->name]) {
                        SymID dependsOn = reg->name;
                        if (state->bindings[reg->name]->dependsOn != NoSymbol)
                            dependsOn = state->bindings[reg->name]->dependsOn;
                        state->bindings[i.dst]->dependsOn = dependsOn;
                    }
                }
            },
            [&](const mir::FieldAddrInst& i) {
                checkOperandAccess(i.object, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
                if (auto* reg = std::get_if<mir::Register>(&i.object)) {
                    if (state->bindings.count(reg->name) && state->bindings[reg->name]) {
                        SymID dependsOn = reg->name;
                        if (state->bindings[reg->name]->dependsOn != NoSymbol)
                            dependsOn = state->bindings[reg->name]->dependsOn;

                        if (state->bindings[reg->name]->fields.count(i.fieldName)
                            && state->bindings[reg->name]->fields[i.fieldName]) {
                            if (state->bindings[reg->name]->fields[i.fieldName]->dependsOn
                                != NoSymbol) {
                                dependsOn
                                    = state->bindings[reg->name]->fields[i.fieldName]->dependsOn;
                            }
                            state->bindings[i.dst]->aliasOf
                                = state->bindings[reg->name]->fields[i.fieldName]->aliasOf;
                        }
                        state->bindings[i.dst]->dependsOn = dependsOn;
                    }
                }
            },
            [&](const mir::IndexAddrInst& i) {
                checkOperandAccess(i.source, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
                if (auto* reg = std::get_if<mir::Register>(&i.source)) {
                    if (state->bindings.count(reg->name) && state->bindings[reg->name]) {
                        SymID dependsOn = reg->name;
                        if (state->bindings[reg->name]->dependsOn != NoSymbol)
                            dependsOn = state->bindings[reg->name]->dependsOn;
                        state->bindings[i.dst]->dependsOn = dependsOn;
                    }
                }
            },
            [&](const mir::CallInst& i) {
                checkOperandAccess(i.function, state, pos);
                for (const auto& arg : i.arguments)
                    checkOperandAccess(arg, state, pos);
                if (i.dst != NoSymbol) {
                    releaseLocksHeldBy(i.dst);
                    initOrRevive(i.dst);
                    if (auto* reg = std::get_if<mir::Register>(&i.function)) {
                        std::string_view name = sym_.resolve(reg->name);
                        if (name == "maml_vec_get" || name == "maml_map_get") {
                            if (!i.arguments.empty()) {
                                if (auto* argReg = std::get_if<mir::Register>(&i.arguments[0])) {
                                    state->bindings[i.dst]->dependsOn = argReg->name;
                                }
                            }
                        }
                    }
                }
            },
            [&](const mir::LoadPtrInst& i) {
                checkOperandAccess(i.ptr, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
                if (auto* ptrReg = std::get_if<mir::Register>(&i.ptr)) {
                    if (state->bindings.count(ptrReg->name) && state->bindings[ptrReg->name]
                        && state->bindings[ptrReg->name]->dependsOn != NoSymbol) {
                        if (isRef(i.dst)) {
                            state->bindings[i.dst]->dependsOn
                                = state->bindings[ptrReg->name]->dependsOn;
                            state->bindings[i.dst]->state = LockState::SharedRead;
                        }
                    }
                }
            },
            [&](const mir::BinaryOpInst& i) {
                checkOperandAccess(i.left, state, pos);
                checkOperandAccess(i.right, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
            },
            [&](const mir::UnaryOpInst& i) {
                checkOperandAccess(i.operand, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
            },
            [&](const mir::CastInst& i) {
                checkOperandAccess(i.src, state, pos);
                releaseLocksHeldBy(i.dst);
                initOrRevive(i.dst);
            },
            [&](const mir::StoreInst& i) {
                checkOperandAccess(i.dstPtr, state, pos);
                checkOperandAccess(i.value, state, pos);
            },
            [&](auto&) {} },
        inst);
}

void BorrowAnalyzer::analyzeTerminator(
    const mir::Terminator& term, BlockState* state, const std::unordered_set<SymID>& liveOut)
{
    Position pos = mir::getPosOf(term);

    std::visit(
        overloaded { [&](std::monostate) {},
            [&](const mir::CoroSuspendTerminator& t) {
                std::vector<SymID> names;
                for (const auto& [name, _] : state->bindings)
                    names.push_back(name);
                std::sort(names.begin(), names.end(),
                    [&](SymID a, SymID b) { return sym_.resolve(a) < sym_.resolve(b); });

                for (SymID name : names) {
                    const auto& binding = state->bindings[name];
                    if (!binding)
                        continue;
                    if (binding->mutLockedBy != NoSymbol
                        && liveOut.find(binding->mutLockedBy) != liveOut.end()) {
                        errorf(pos,
                            std::format("mutable reference '{}' borrowing '{}' cannot be held "
                                        "across an `await` point",
                                sym_.resolve(binding->mutLockedBy), sym_.resolve(name)));
                    }
                    if (binding->dependsOn != NoSymbol && liveOut.find(name) != liveOut.end()) {
                        errorf(pos,
                            std::format(
                                "borrow or view '{}' cannot be held across an `await` point",
                                sym_.resolve(name)));
                    }
                }
            },
            [&](const mir::ReturnTerminator& t) { checkOperandAccess(t.value, state, pos); },
            [&](const mir::BranchTerminator& t) { checkOperandAccess(t.condition, state, pos); },
            [&](auto&) {} },
        term);
}

// =============================================================================
// Validation Handlers
// =============================================================================

void BorrowAnalyzer::checkOperandAccess(const mir::Value& op, BlockState* state, Position pos)
{
    if (auto* reg = std::get_if<mir::Register>(&op)) {
        checkStringAccess(reg->name, state, pos);
    }
}

void BorrowAnalyzer::checkStringAccess(SymID name, BlockState* state, Position pos)
{
    if (isCompilerGenerated(name))
        return;
    auto it = state->bindings.find(name);
    if (it == state->bindings.end() || !it->second)
        return;

    BindingState* binding = it->second.get();
    LockState agg = binding->aggregateState();

    if (agg == LockState::Invalidated) {
        if (binding->dependsOn != NoSymbol) {
            errorf(pos,
                std::format("use of invalidated view '{}' (its parent buffer '{}' was dropped)",
                    sym_.resolve(name), sym_.resolve(binding->dependsOn)));
        } else {
            errorf(pos, std::format("use of moved variable '{}'", sym_.resolve(name)));
        }
    } else if (agg == LockState::MaybeInvalidated) {
        errorf(pos,
            std::format(
                "use of conditionally moved (MaybeInvalidated) variable '{}'", sym_.resolve(name)));
    } else if (binding->mutLockedBy != NoSymbol) {
        errorf(pos,
            std::format(
                "cannot access variable '{}' because it is currently mutably borrowed by '{}'",
                sym_.resolve(name), sym_.resolve(binding->mutLockedBy)));
    }
}

void BorrowAnalyzer::validateScopeExit(const mir::Graph* g,
    const std::unordered_map<mir::BlockID, std::unique_ptr<BlockState>>& stateOut)
{
    std::unordered_set<SymID> isParam;
    for (const auto& p : g->params)
        isParam.insert(p.name);

    for (const auto& [id, block] : g->blocks) {
        if (auto* ret = std::get_if<mir::ReturnTerminator>(&block->terminator)) {
            const auto& state = stateOut.at(id);
            SymID retName = NoSymbol;
            if (auto* reg = std::get_if<mir::Register>(&ret->value))
                retName = reg->name;

            for (const auto& [name, binding] : state->bindings) {
                if (binding && (name == retName || isParam.count(name))) {
                    auditBinding(name, binding.get(), isParam, mir::getPosOf(block->terminator));
                }
            }
        }
    }
}

void BorrowAnalyzer::auditBinding(
    SymID name, const BindingState* b, const std::unordered_set<SymID>& isParam, Position pos)
{
    if (b->state == LockState::MaybeInvalidated) {
        errorf(pos,
            std::format("binding '{}' is in a conditionally moved (MaybeInvalidated) state and "
                        "cannot be returned.",
                sym_.resolve(name)));
    }
    if (b->dependsOn != NoSymbol && isParam.find(b->dependsOn) == isParam.end()) {
        errorf(pos,
            std::format("Lifetime Escape Error: cannot return view '{}' because it depends on "
                        "local variable '{}' which will be dropped",
                sym_.resolve(name), sym_.resolve(b->dependsOn)));
    }
    for (const auto& [fieldName, field] : b->fields) {
        auditBinding(sym_.intern(std::string(sym_.resolve(name)) + "."
                         + std::string(sym_.resolve(fieldName))),
            field.get(), isParam, pos);
    }
}

} // namespace maml::passes