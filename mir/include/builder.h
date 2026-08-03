#pragma once

#include "ast.h"
#include "ast_nodes.h"
#include "cfg.h"
#include "mir.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"
#include <memory>
#include <tuple>
#include <type_traits>
#include <unordered_map>
#include <variant>
#include <vector>

namespace maml::mir {

struct Target {
    int pointerSize = 8;
    int pointerAlign = 8;
    int intSize = 8;
};

extern const Target DefaultTarget;

struct LoopTracker {
    BlockID header;
    BlockID exit;
};

// Trait to detect whether an AST node has exprType
template <typename T> struct HasExprType {
private:
    template <typename U>
    static auto test(int) -> decltype(std::declval<U*>()->exprType, std::true_type());
    template <typename> static std::false_type test(...);

public:
    static constexpr bool value = decltype(test<T>(0))::value;
};

// Safely extract exprType from an AST expression variant
template <typename Variant> const types::Type* safeGetExprType(const Variant& v)
{
    return std::visit(
        [](auto&& e) -> const types::Type* {
            using T = std::decay_t<decltype(e)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                return nullptr;
            } else if constexpr (std::is_same_v<T, ast::TypeExprWrapper*>) {
                return e ? e->exprType : nullptr;
            } else if constexpr (HasExprType<std::remove_pointer_t<T>>::value) {
                return e ? e->exprType : nullptr;
            } else {
                return nullptr;
            }
        },
        v);
}

class Builder {
public:
    Builder(types::TypeRegistry& reg, SymbolTable& sym, const Target& target = DefaultTarget);

    std::unique_ptr<Program> buildProgram(ast::Program* astProg);

private:
    types::TypeRegistry& reg_;
    SymbolTable& sym_;
    const Target& target_;

    // Per-function CFG state
    Graph* graph_ = nullptr;
    BlockID nextID_ = 0;
    std::vector<LoopTracker> loops_;
    int tempCount_ = 0;

    // Lexical shadowing and unique name generation
    std::vector<std::unordered_map<SymID, SymID>> env_;
    std::unordered_map<SymID, int> nameFreq_;
    std::unordered_map<SymID, const types::Type*> locals_;

    BasicBlock* current_ = nullptr;
    Value currentFuture_ = std::monostate {};

    // --- Core Primitives & State Management ---
    void buildFn(ast::FnDecl* fn, Program& prog);
    void enterScope();
    void exitScope();
    SymID defineLocal(SymID originalName);
    SymID resolveLocal(SymID originalName);

    BasicBlock* newBlock();
    SymID newTemp();
    SymID newPtrTemp();
    SymID emitTemp(const types::Type* t);

    void push(const Instruction& inst);
    Value emit(Instruction inst, SymID dst, const types::Type* t);
    Value emitLoad(Value ptr, const types::Type* t, Position pos);
    Value emitFieldAddr(Value obj, const types::Type* objType, SymID fieldName, int fieldIdx,
        const types::Type* fieldType, Position pos);
    void storeField(Value obj, const types::Type* objType, Value val, SymID fieldName, int fieldIdx,
        const types::Type* fieldType, Position pos);
    Value loadField(Value obj, const types::Type* objType, SymID fieldName, int fieldIdx,
        const types::Type* fieldType, Position pos);
    Value emitGetFutureResult(Value futureVal, const types::Type* resultType, Position pos);
    Value flattenStringEq(ast::InfixExpr* e, Value flatLeft, Value flatRight);

    Value emitBorrow(SymID src, bool isMut, Position pos);
    void emitTransfer(SymID dst, Value val, Position pos);
    void emitCapTransfer(SymID dst, SymID src, ast::Capability cap, Position pos);
    BasicBlock* emitCoroSuspend(Position pos);
    Value emitCompoundMath(
        Value ptrVal, TokenType op, ast::Expr rhsExpr, const types::Type* elemType, Position pos);

    const types::Type* lowerParamType(const types::Type* t, ast::Capability cap);
    bool ownsHeapMemory(const types::Type* t) const;
    static bool isAggregateType(const types::Type* t);
    Value boxScalar(Value val, const types::Type* t, Position pos);
    Value emitRuntimeCall(
        SymID funcSym, const types::Type* retType, const std::vector<Value>& args, Position pos);

    void emitVariantInit(BasicBlock* block, SymID dst, const types::Type* sumType,
        SymID variantName, int discriminant, const std::vector<Value>& payloads, Position pos);
    const types::Type* getVariantPayloadStructType(const types::Type* t, SymID variantName);

    // --- Lowering passes (To be implemented in Phase 4/5) ---
    Value lowerExpr(ast::Expr expr);
    void lowerStmt(ast::Stmt stmt);
    void lowerBlockStmt(ast::BlockStmt* block);
    Value addressOf(ast::Expr expr);

    // Extracted Intrinsic and ABI Calls
    Value EmitMamlVecPush(Value vec, Value element, Position pos);
    Value EmitMamlVecGet(Value vec, Value index, Position pos);
    Value EmitMamlVecSet(Value vec, Value index, Value element, Position pos);
    Value EmitMamlMapPut(
        Value m, Value key_hash, Value key_ptr, Value key_len, Value val_ptr, Position pos);
    Value EmitMamlMapGet(Value m, Value key_hash, Value key_ptr, Value key_len, Position pos);

    // Composite literal lowering
    Value lowerVecLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType);
    Value lowerMapLiteral(ast::CompositeLiteral* e, const types::Type* resolvedType);
    std::tuple<Value, Value, Value> lowerMapKey(ast::Expr keyExpr, Position pos);
};

} // namespace maml::mir