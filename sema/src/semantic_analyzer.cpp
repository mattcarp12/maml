#include "semantic_analyzer.h"
#include "ast.h"
#include "passes.h"

namespace maml::sema {

SemanticAnalyzer::SemanticAnalyzer(CompilerContext& ctx)
    : ctx_(ctx)
{
}

bool SemanticAnalyzer::analyze(ast::Program* program)
{
    if (!program) {
        ctx_.diagnostics.error("Null program provided to SemanticAnalyzer");
        return false;
    }

    // Pass 1: Hoist symbols and type declarations into scope
    DeclarationPass declPass(ctx_);
    declPass.run(program);
    if (ctx_.diagnostics.hasErrors()) {
        return false;
    }

    // Pass 2: Evaluate type expressions and compute layout structures
    TypeResolutionPass typeResPass(ctx_);
    typeResPass.run(program);
    if (ctx_.diagnostics.hasErrors()) {
        return false;
    }

    // Pass 3: Recursive type checking, side-table decoration, and argument checks
    TypeCheckPass typeCheckPass(ctx_);
    typeCheckPass.run(program);
    if (ctx_.diagnostics.hasErrors()) {
        return false;
    }

    // Pass 4: Control flow verification, match exhaustiveness, capability safety
    ControlFlowPass cfPass(ctx_);
    cfPass.run(program);
    if (ctx_.diagnostics.hasErrors()) {
        return false;
    }

    // Pass 5: Desugar pass
    DesugarPass desugarPass(ctx_);
    desugarPass.run(program);

    return !ctx_.diagnostics.hasErrors();
}

} // namespace maml::sema