#pragma once

#include "cfg.h"
#include "liveness.h"
#include "mir.h"
#include "sym.h"
#include "type_registry.h"
#include "types.h"
#include <unordered_map>
#include <unordered_set>
#include <vector>

namespace maml::passes {

class DropInjector {
public:
    DropInjector(SymbolTable& sym, types::TypeRegistry& reg)
        : sym_(sym)
        , reg_(reg)
        , dropCounter_(0)
    {
    }

    void injectDrops(mir::Graph* g, std::unordered_map<SymID, const types::Type*>& locals,
        const LivenessResult& globalLiveness);

private:
    SymbolTable& sym_;
    types::TypeRegistry& reg_;
    int dropCounter_;

    SymID newDropTemp();

    std::unordered_map<SymID, const types::Type*> buildTypeEnv(
        const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals) const;
    std::unordered_set<SymID> buildViewSet(
        const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals) const;

    bool hasLiveAlias(SymID v, const std::unordered_set<SymID>& liveSet,
        const std::unordered_map<SymID, std::vector<SymID>>& revAliases) const;

    void buildRecursiveDrop(SymID vName, const types::Type* t, bool isAddr,
        std::vector<mir::Instruction>& stmts, std::unordered_map<SymID, const types::Type*>& locals,
        Position pos);

    std::string_view lookupDestructorSymbol(const types::Type* t) const;
    bool needsDrop(const types::Type* t) const;
    bool isPrimitive(const types::Type* t) const;
    SymID getDef(const mir::Instruction& inst) const;
};

} // namespace maml::passes