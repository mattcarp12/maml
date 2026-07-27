#include "drop_injector.h"

namespace maml::passes {

template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

void DropInjector::injectDrops(mir::Graph* g, std::unordered_map<SymID, const types::Type*>& locals,
    const LivenessResult& globalLiveness)
{
    auto env = buildTypeEnv(g, locals);
    auto views = buildViewSet(g, locals);

    // Build reverse alias map: src -> []dst (all variables that alias src)[cite: 3]
    std::unordered_map<SymID, std::vector<SymID>> revAliases;
    for (const auto& [dst, src] : globalLiveness.aliases) {
        revAliases[src].push_back(dst);
    }

    LivenessAnalyzer livenessAnalyzer(sym_);

    for (mir::BasicBlock* block : g->sortedBlocks()) {
        auto stmtLiveness = livenessAnalyzer.analyzeStatementLiveness(
            block, globalLiveness.liveOut.at(block->id), globalLiveness.aliases);
        std::vector<mir::Instruction> newStmts;

        for (size_t i = 0; i < block->statements.size(); ++i) {
            const auto& inst = block->statements[i];
            newStmts.push_back(inst);
            Position pos = mir::getPosOf(inst);

            std::vector<SymID> dying;
            for (SymID v : stmtLiveness.liveIn[i]) {
                if (stmtLiveness.liveOut[i].find(v) == stmtLiveness.liveOut[i].end()
                    && views.find(v) == views.end()) {
                    if (!hasLiveAlias(v, stmtLiveness.liveOut[i], revAliases)) {

                        // Suppress drop if consumed by a CallInst[cite: 3]
                        bool consumedByCall = false;
                        if (auto* callInst = std::get_if<mir::CallInst>(&inst)) {
                            for (size_t argIdx = 0; argIdx < callInst->arguments.size(); ++argIdx) {
                                if (auto* argReg
                                    = std::get_if<mir::Register>(&callInst->arguments[argIdx])) {
                                    if (argReg->name == v && argIdx < callInst->argConsumed.size()
                                        && callInst->argConsumed[argIdx]) {
                                        consumedByCall = true;
                                        break;
                                    }
                                }
                            }
                        }

                        if (!consumedByCall) {
                            dying.push_back(v);
                        }
                    }
                }
            }

            SymID def = getDef(inst);
            if (def != NoSymbol
                && stmtLiveness.liveOut[i].find(def) == stmtLiveness.liveOut[i].end()
                && views.find(def) == views.end()) {
                if (!hasLiveAlias(def, stmtLiveness.liveOut[i], revAliases)) {
                    dying.push_back(def);
                }
            }

            for (SymID v : dying) {
                auto it = env.find(v);
                if (it == env.end() || !needsDrop(it->second))
                    continue;

                if (auto* moveInst = std::get_if<mir::MoveInst>(&inst)) {
                    if (moveInst->src == v)
                        continue;
                }
                if (auto* storeInst = std::get_if<mir::StoreInst>(&inst)) {
                    if (auto* srcReg = std::get_if<mir::Register>(&storeInst->value)) {
                        if (srcReg->name == v)
                            continue;
                    }
                }

                buildRecursiveDrop(v, it->second, false, newStmts, locals, pos);
            }
        }

        std::vector<SymID> termDying;
        for (SymID v : stmtLiveness.termLiveIn) {
            if (stmtLiveness.termLiveOut.find(v) == stmtLiveness.termLiveOut.end()
                && views.find(v) == views.end()) {
                if (!hasLiveAlias(v, stmtLiveness.termLiveOut, revAliases)) {
                    termDying.push_back(v);
                }
            }
        }

        Position termPos = mir::getPosOf(block->terminator);
        for (SymID v : termDying) {
            auto it = env.find(v);
            if (it == env.end() || !needsDrop(it->second))
                continue;

            if (auto* retTerm = std::get_if<mir::ReturnTerminator>(&block->terminator)) {
                if (auto* reg = std::get_if<mir::Register>(&retTerm->value)) {
                    if (reg->name == v)
                        continue;
                }
            }

            buildRecursiveDrop(v, it->second, false, newStmts, locals, termPos);
        }

        block->statements = std::move(newStmts);
    }
}

bool DropInjector::hasLiveAlias(SymID v, const std::unordered_set<SymID>& liveSet,
    const std::unordered_map<SymID, std::vector<SymID>>& revAliases) const
{
    std::unordered_set<SymID> visited;

    auto dfs = [&](auto& self, SymID current) -> bool {
        if (visited.find(current) != visited.end())
            return false;
        visited.insert(current);

        auto it = revAliases.find(current);
        if (it != revAliases.end()) {
            for (SymID alias : it->second) {
                if (liveSet.find(alias) != liveSet.end())
                    return true;
                if (self(self, alias))
                    return true;
            }
        }
        return false;
    };

    return dfs(dfs, v);
}

SymID DropInjector::newDropTemp()
{
    dropCounter_++;
    return sym_.intern("_drop_t" + std::to_string(dropCounter_));
}

void DropInjector::buildRecursiveDrop(SymID vName, const types::Type* t, bool isAddr,
    std::vector<mir::Instruction>& stmts, std::unordered_map<SymID, const types::Type*>& locals,
    Position pos)
{
    if (!needsDrop(t))
        return;

    if (t->kind == types::TypeKind::Struct) {
        const auto& fields = std::get<types::StructPayload>(t->payload).fields;
        for (size_t i = 0; i < fields.size(); ++i) {
            if (needsDrop(fields[i].type)) {
                SymID tmpPtr = newDropTemp();
                locals[tmpPtr] = reg_.getPrimitive(types::TypeKind::Ptr);

                stmts.push_back(mir::FieldAddrInst { tmpPtr, mir::Register { vName, t, pos }, t,
                    fields[i].name, static_cast<int>(i), fields[i].type, {}, pos });
                buildRecursiveDrop(tmpPtr, fields[i].type, true, stmts, locals, pos);
            }
        }
        return;
    }

    if (t->kind == types::TypeKind::Array) {
        const auto& payload = std::get<types::ArrayPayload>(t->payload);
        for (int i = 0; i < payload.size; i++) {
            SymID tmpPtr = newDropTemp();
            locals[tmpPtr] = reg_.getPrimitive(types::TypeKind::Ptr);

            stmts.push_back(mir::IndexAddrInst { tmpPtr, mir::Register { vName, t, pos }, t,
                mir::IntConstant {
                    static_cast<int64_t>(i), reg_.getPrimitive(types::TypeKind::I64), pos },
                payload.base, pos });
            buildRecursiveDrop(tmpPtr, payload.base, true, stmts, locals, pos);
        }
        return;
    }

    std::string_view symbol = lookupDestructorSymbol(t);
    if (!symbol.empty()) {
        SymID callArgName = NoSymbol;

        if (isAddr) {
            callArgName = vName;
        } else {
            SymID ptrTmp = newDropTemp();
            locals[ptrTmp] = reg_.getPrimitive(types::TypeKind::Ptr);
            stmts.push_back(mir::BorrowInst { ptrTmp, false, vName, pos });
            callArgName = ptrTmp;
        }

        std::vector<mir::Value> args
            = { mir::Register { callArgName, reg_.getPrimitive(types::TypeKind::Ptr), pos } };
        std::vector<bool> consumed = { false };

        stmts.push_back(mir::CallInst { sym_.intern("_"),
            mir::Register {
                sym_.intern(std::string(symbol)), reg_.getPrimitive(types::TypeKind::Ptr), pos },
            args, consumed, reg_.getPrimitive(types::TypeKind::Unit), pos });
    }
}

std::string_view DropInjector::lookupDestructorSymbol(const types::Type* t) const
{
    switch (t->kind) {
    case types::TypeKind::Vector:
        return "maml_vec_free";
    case types::TypeKind::Map:
        return "maml_map_free";
    case types::TypeKind::Future:
        return "maml_task_release";
    case types::TypeKind::String:
        return "maml_str_free";
    case types::TypeKind::Sum:
        return "";
    default:
        return "maml_free";
    }
}

std::unordered_set<SymID> DropInjector::buildViewSet(
    const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals) const
{
    std::unordered_set<SymID> views;
    std::unordered_set<SymID> addrIsDerived;

    for (const auto& [name, t] : locals) {
        if (t && t->kind == types::TypeKind::Ptr)
            addrIsDerived.insert(name);
    }
    for (const auto& p : g->params) {
        if (p.type && p.type->kind == types::TypeKind::Ptr)
            addrIsDerived.insert(p.name);
    }

    for (const auto& [_, block] : g->blocks) {
        for (const auto& inst : block->statements) {
            std::visit(overloaded { [&](std::monostate) {},
                           [&](const mir::FieldAddrInst& i) { addrIsDerived.insert(i.dst); },
                           [&](const mir::IndexAddrInst& i) { addrIsDerived.insert(i.dst); },
                           [&](const mir::BorrowInst& i) { addrIsDerived.insert(i.dst); },
                           [&](const mir::LoadPtrInst& i) {
                               if (auto* ptrReg = std::get_if<mir::Register>(&i.ptr)) {
                                   if (addrIsDerived.find(ptrReg->name) != addrIsDerived.end()) {
                                       views.insert(i.dst);
                                   }
                               }
                           },
                           [&](auto&) {} },
                inst);
        }
    }
    return views;
}

std::unordered_map<SymID, const types::Type*> DropInjector::buildTypeEnv(
    const mir::Graph* g, const std::unordered_map<SymID, const types::Type*>& locals) const
{
    std::unordered_map<SymID, const types::Type*> env = locals;
    for (const auto& p : g->params)
        env[p.name] = p.type;

    for (const auto& [_, block] : g->blocks) {
        for (const auto& inst : block->statements) {
            std::visit(
                overloaded { [&](std::monostate) {},
                    [&](const mir::CastInst& i) { env[i.dst] = i.type; },
                    [&](const mir::FieldAddrInst& i) {
                        env[i.dst] = reg_.getPrimitive(types::TypeKind::Ptr);
                    },
                    [&](const mir::IndexAddrInst& i) {
                        env[i.dst] = reg_.getPrimitive(types::TypeKind::Ptr);
                    },
                    [&](const mir::BitcastPtrInst& i) { env[i.dst] = i.type; }, [&](auto&) {} },
                inst);
        }
    }
    return env;
}

bool DropInjector::needsDrop(const types::Type* t) const
{
    if (!t || isPrimitive(t))
        return false;

    switch (t->kind) {
    case types::TypeKind::View:
        return false;
    case types::TypeKind::Vector:
    case types::TypeKind::Map:
    case types::TypeKind::Future:
    case types::TypeKind::String:
        return true;
    case types::TypeKind::Struct:
        for (const auto& field : std::get<types::StructPayload>(t->payload).fields) {
            if (needsDrop(field.type))
                return true;
        }
        return false;
    case types::TypeKind::Array:
        return needsDrop(std::get<types::ArrayPayload>(t->payload).base);
    case types::TypeKind::Sum:
        for (const auto& variant : std::get<types::SumPayload>(t->payload).variants) {
            for (const auto& field : variant.fields) {
                if (needsDrop(field.type))
                    return true;
            }
            for (const auto& tupleTy : variant.tupleTypes) {
                if (needsDrop(tupleTy))
                    return true;
            }
        }
        return false;
    default:
        return true;
    }
}

bool DropInjector::isPrimitive(const types::Type* t) const
{
    switch (t->kind) {
    case types::TypeKind::I8:
    case types::TypeKind::I16:
    case types::TypeKind::I32:
    case types::TypeKind::I64:
    case types::TypeKind::I128:
    case types::TypeKind::U8:
    case types::TypeKind::U16:
    case types::TypeKind::U32:
    case types::TypeKind::U64:
    case types::TypeKind::U128:
    case types::TypeKind::F32:
    case types::TypeKind::F64:
    case types::TypeKind::Bool:
    case types::TypeKind::Char:
    case types::TypeKind::Unit:
    case types::TypeKind::Ptr:
        return true;
    default:
        return false;
    }
}

SymID DropInjector::getDef(const mir::Instruction& inst) const
{
    return std::visit(overloaded { [&](std::monostate) -> SymID { return NoSymbol; },
                          [&](const mir::AllocaInst& i) { return i.dst; },
                          [&](const mir::AssignInst& i) { return i.dst; },
                          [&](const mir::CopyInst& i) { return i.dst; },
                          [&](const mir::MoveInst& i) { return i.dst; },
                          [&](const mir::BorrowInst& i) { return i.dst; },
                          [&](const mir::CallInst& i) { return i.dst; },
                          [&](const mir::FieldAddrInst& i) { return i.dst; },
                          [&](const mir::BinaryOpInst& i) { return i.dst; },
                          [&](const mir::UnaryOpInst& i) { return i.dst; },
                          [&](const mir::CastInst& i) { return i.dst; },
                          [&](const mir::LoadPtrInst& i) { return i.dst; },
                          [&](const mir::BitcastPtrInst& i) { return i.dst; },
                          [&](const mir::IndexAddrInst& i) { return i.dst; },
                          [&](auto&) -> SymID { return NoSymbol; } },
        inst);
}

} // namespace maml::passes