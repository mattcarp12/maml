#pragma once

#include "ast_nodes.h"
#include "mir.h"
#include "sym.h"
#include "types.h"
#include <memory>
#include <unordered_map>
#include <vector>

namespace maml::mir {

struct BasicBlock {
    BlockID id;
    std::vector<Instruction> statements;
    Terminator terminator;
};

struct Param {
    SymID name;
    const types::Type* type = nullptr;
};

class Graph {
public:
    BlockID entry = InvalidBlock;
    std::vector<Param> params;
    std::unordered_map<BlockID, std::unique_ptr<BasicBlock>> blocks;

    // Returns blocks sorted by ID for deterministic emission to LLVM
    std::vector<BasicBlock*> sortedBlocks() const;
};

struct Function {
    SymID name;
    std::vector<Param> params;
    const types::Type* returnType = nullptr;
    bool isAsync = false;
    bool isExtern = false;

    std::unique_ptr<Graph> graph;
    std::unordered_map<SymID, const types::Type*> locals;
};

struct Program {
    // Because we skipped HIR, we can just point directly to the AST TypeDecls
    std::vector<ast::TypeDecl*> typeDecls;
    std::vector<Function> functions;
};

} // namespace maml::mir