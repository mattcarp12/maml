#pragma once

#include "ast.h"
#include "compiler_context.h"

namespace maml::sema {

class SemanticAnalyzer {
public:
    explicit SemanticAnalyzer(CompilerContext& ctx);

    // Runs all 5 semantic pipeline passes sequentially on the program AST.
    // Returns true if analysis completed without fatal errors.
    bool analyze(ast::Program* program);

private:
    CompilerContext& ctx_;
};

} // namespace maml::sema