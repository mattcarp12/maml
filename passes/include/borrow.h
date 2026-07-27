#pragma once

#include "ast.h"
#include "cfg.h"
#include "liveness.h"
#include "mir.h"
#include "sym.h"
#include "types.h"
#include <memory>
#include <string>
#include <unordered_map>
#include <vector>

namespace maml::passes {

enum class LockState { ExclusiveWrite, SharedRead, MaybeInvalidated, Invalidated };

std::string toString(LockState s);
LockState joinStates(LockState s1, LockState s2);

struct BindingState {
    LockState state = LockState::ExclusiveWrite;
    SymID mutLockedBy = NoSymbol;
    SymID dependsOn = NoSymbol;
    SymID aliasOf = NoSymbol;
    std::unordered_map<SymID, std::unique_ptr<BindingState>> fields;

    LockState aggregateState() const;
    std::unique_ptr<BindingState> clone() const;
};

struct BlockState {
    std::unordered_map<SymID, std::unique_ptr<BindingState>> bindings;

    std::unique_ptr<BlockState> clone() const;
};

class BorrowAnalyzer {
public:
    BorrowAnalyzer(SymbolTable& sym)
        : sym_(sym)
        , reportErrors_(false)
    {
    }

    std::vector<ast::CompileError> analyze(const mir::Graph* g,
        const std::unordered_map<SymID, const types::Type*>& locals, const LivenessResult& live);

private:
    SymbolTable& sym_;
    std::vector<ast::CompileError> errors_;
    std::unordered_map<SymID, const types::Type*> varTypes_;
    bool reportErrors_;

    bool isRef(SymID name) const;
    bool isCompilerGenerated(SymID name) const;
    void errorf(Position pos, const std::string& msg);

    void runBlock(const mir::BasicBlock* block, BlockState* currentState,
        const BlockStatementLiveness& stmtLiveness, const LivenessResult& live);
    std::unique_ptr<BlockState> mergePredecessors(const mir::Graph* g, const mir::BasicBlock* block,
        const std::unordered_map<mir::BlockID, std::unique_ptr<BlockState>>& stateOut,
        const std::unordered_map<mir::BlockID, bool>& visited);

    std::unique_ptr<BindingState> mergeBindings(const BindingState* b1, const BindingState* b2);
    bool statesEqual(const BlockState* s1, const BlockState* s2) const;
    bool bindingsEqual(const BindingState* b1, const BindingState* b2) const;

    std::vector<mir::BlockID> getPredecessors(const mir::Graph* g, mir::BlockID target) const;
    std::vector<mir::BlockID> getSuccessors(const mir::BasicBlock* block) const;

    void analyzeInstruction(const mir::Instruction& inst, BlockState* state);
    void analyzeTerminator(
        const mir::Terminator& term, BlockState* state, const std::unordered_set<SymID>& liveOut);

    void checkOperandAccess(const mir::Value& op, BlockState* state, Position pos);
    void checkStringAccess(SymID name, BlockState* state, Position pos);

    void validateScopeExit(const mir::Graph* g,
        const std::unordered_map<mir::BlockID, std::unique_ptr<BlockState>>& stateOut);
    void auditBinding(
        SymID name, const BindingState* b, const std::unordered_set<SymID>& isParam, Position pos);
};

} // namespace maml::passes