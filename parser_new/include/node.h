// node.h
#pragma once
#include <cstdint>

namespace maml::ast {

using NodeID = uint32_t;
constexpr NodeID NoNode = 0;

// Every AST node inherits this. Enables CompilerContext side-tables
// (types, resolved symbols, constants, lvalue-ness) keyed by node id,
// instead of storing semantic state on the node itself.
struct NodeBase {
    NodeID id = NoNode;
};

} // namespace maml::ast
