#pragma once
#include "ast.h"
#include "cfg.h"
#include "sym.h"
#include "type_registry.h"
#include "types.h"
#include <unordered_map>
#include <vector>

namespace maml::passes {

struct PassConfig {
    bool liveness = true;
    bool borrow = true;
    bool linearLower = true;
    bool dropInject = true;
};

PassConfig defaultConfig();

std::vector<ast::CompileError> runPasses(mir::Graph* g,
    std::unordered_map<SymID, const types::Type*>& locals, SymbolTable& sym,
    types::TypeRegistry& reg, const PassConfig& cfg = defaultConfig());

} // namespace maml::passes