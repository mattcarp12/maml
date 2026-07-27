#include "pipeline.h"
#include "borrow.h"
#include "drop_injector.h"
#include "linear_lower.h"
#include "liveness.h"

namespace maml::passes {

PassConfig defaultConfig() { return PassConfig { true, true, true, true }; }

std::vector<ast::CompileError> runPasses(mir::Graph* g,
    std::unordered_map<SymID, const types::Type*>& locals, SymbolTable& sym,
    types::TypeRegistry& reg, const PassConfig& cfg)
{
    if (!g)
        return {};

    std::unique_ptr<LivenessResult> livenessRes = nullptr;
    if (cfg.liveness) {
        LivenessAnalyzer livenessAnalyzer(sym);
        livenessRes = std::make_unique<LivenessResult>(livenessAnalyzer.analyzeLiveness(g, locals));
    }

    std::vector<ast::CompileError> borrowErrs;
    if (cfg.borrow && livenessRes) {
        BorrowAnalyzer borrowAnalyzer(sym);
        borrowErrs = borrowAnalyzer.analyze(g, locals, *livenessRes);
    }

    if (cfg.dropInject && livenessRes) {
        DropInjector dropInjector(sym, reg);
        dropInjector.injectDrops(g, locals, *livenessRes);
    }

    if (cfg.linearLower) {
        lowerLinearTypes(g);
    }

    return borrowErrs;
}

} // namespace maml::passes