#include "ast.h"

#include "builder.h"
#include "cfg.h"
#include "mir.h"
#include "sym.h"
#include "token.h"
#include "types.h"
#include <cstddef>
#include <cstdint>
#include <string>
#include <string_view>
#include <tuple>
#include <type_traits>
#include <variant>
#include <vector>

namespace maml::mir {

template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

// =============================================================================
// Helper: Compound Math (e.g., +=, -=)
// =============================================================================

Value Builder::emitCompoundMath(
    Value ptrVal, TokenType op, ast::Expr rhsExpr, const types::Type* elemType, Position pos)
{
    Value readVal = emitLoad(ptrVal, elemType, pos);
    Value flatRHS = lowerExpr(rhsExpr);

    SymID opTmp = newTemp();
    return emit(BinaryOpInst { .dst = opTmp,
                    .left = readVal,
                    .op = op,
                    .right = flatRHS,
                    .type = elemType,
                    .pos = pos },
        opTmp, elemType);
}

// =============================================================================
// Statement Lowering
// =============================================================================

void Builder::lowerBlockStmt(ast::BlockStmt* block)
{
    if (!block || block->statements.empty())
        return;

    for (size_t i = 0; i < block->statements.size(); ++i) {
        if (!current_)
            break; // Stop emitting if the block has terminated (e.g., after a return)

        ast::Stmt stmt = block->statements[i];

        // If it's the last statement and it's a Yield or Expr, we evaluate it directly
        if (i == block->statements.size() - 1) {
            if (auto** yieldStmt = std::get_if<ast::YieldStmt*>(&stmt)) {
                lowerExpr((*yieldStmt)->value);
                continue;
            }
            if (auto** exprStmt = std::get_if<ast::ExprStmt*>(&stmt)) {
                lowerExpr((*exprStmt)->value);
                continue;
            }
        }

        lowerStmt(stmt);
    }
}

void Builder::lowerStmt(ast::Stmt stmt)
{
    if (!current_)
        return;

    std::visit(
        overloaded {
            [&](std::monostate) {},
            [&](ast::BlockStmt* s) {
                enterScope();
                lowerBlockStmt(s);
                exitScope();
            },
            [&](ast::DeclareStmt* s) {
                Value flatRHS = lowerExpr(s->value);
                SymID uniqueName = defineLocal(s->name);

                const types::Type* t = reg_.getPrimitive(types::TypeKind::Unknown);
                if (!std::holds_alternative<std::monostate>(s->value)) {
                    t = safeGetExprType(s->value);
                }
                bool needsAlloca = s->isMutable || ownsHeapMemory(t)
                    || (t
                        && (t->kind == types::TypeKind::Struct || t->kind == types::TypeKind::Array
                            || t->kind == types::TypeKind::Sum));

                if (needsAlloca) {
                    locals_[uniqueName] = reg_.getPrimitive(types::TypeKind::Ptr);
                    push(AllocaInst { .dst = uniqueName, .type = t, .pos = s->pos });

                    Value ptrReg = Register { .name = uniqueName,
                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                        .pos = s->pos };
                    push(
                        StoreInst { .dstPtr = ptrReg, .value = flatRHS, .type = t, .pos = s->pos });
                } else {
                    locals_[uniqueName] = t;
                    emitTransfer(uniqueName, flatRHS, s->pos);
                }
            },
            [&](ast::AliasDecl* s) {
                Value flatSrc = lowerExpr(s->value);
                SymID aliasName = defineLocal(s->name);
                locals_[aliasName] = s->cap == ast::Capability::Mut
                    ? reg_.getPrimitive(types::TypeKind::Ptr)
                    : safeGetExprType(s->value);

                if (auto* srcReg = std::get_if<Register>(&flatSrc)) {
                    emitCapTransfer(aliasName, srcReg->name, s->cap, s->pos);
                }
            },
            [&](ast::AssignStmt* s) {
                if (auto** faPtr = std::get_if<ast::FieldAccess*>(&s->lValue)) {
                    ast::FieldAccess* fa = *faPtr;
                    Value ptrVal = addressOf(s->lValue);
                    Value writeVal;
                    if (s->op != TokenType::ASSIGN) {
                        writeVal = emitCompoundMath(ptrVal, s->op, s->rValue, fa->exprType, s->pos);
                    } else {
                        writeVal = lowerExpr(s->rValue);
                    }
                    push(StoreInst {
                        .dstPtr = ptrVal, .value = writeVal, .type = fa->exprType, .pos = s->pos });
                    return;
                }

                if (auto** idxPtr = std::get_if<ast::IndexExpr*>(&s->lValue)) {
                    ast::IndexExpr* idx = *idxPtr;
                    const types::Type* sourceType = safeGetExprType(idx->left);

                    // Handle Map index assignment: m[key] = val
                    if (sourceType && sourceType->kind == types::TypeKind::Map) {
                        Value mapPtr = addressOf(idx->left);
                        auto [hashVal, keyPtrVal, lenVal] = lowerMapKey(idx->index, s->pos);

                        const types::Type* valType
                            = std::get<types::MapPayload>(sourceType->payload).value;

                        Value writeVal = lowerExpr(s->rValue);

                        Value boxedVal;
                        if (valType && isAggregateType(valType)) {
                            if (auto* reg = std::get_if<Register>(&writeVal)) {
                                boxedVal = emitBorrow(reg->name, true, s->pos);
                            } else {
                                boxedVal = boxScalar(writeVal, valType, s->pos);
                            }
                        } else {
                            boxedVal = boxScalar(writeVal, valType, s->pos);
                        }

                        EmitMamlMapPut(mapPtr, hashVal, keyPtrVal, lenVal, boxedVal, s->pos);
                        return;
                    }

                    Value ptrVal = addressOf(s->lValue);
                    Value writeVal;
                    if (s->op != TokenType::ASSIGN) {
                        writeVal
                            = emitCompoundMath(ptrVal, s->op, s->rValue, idx->exprType, s->pos);
                    } else {
                        writeVal = lowerExpr(s->rValue);
                    }
                    push(StoreInst { .dstPtr = ptrVal,
                        .value = writeVal,
                        .type = idx->exprType,
                        .pos = s->pos });
                    return;
                }

                if (auto** idPtr = std::get_if<ast::Identifier*>(&s->lValue)) {
                    ast::Identifier* ident = *idPtr;
                    SymID dstName = resolveLocal(ident->name);

                    // IMPLICIT STORE: If the local is a pointer, mutate the underlying memory
                    if (locals_[dstName]->kind == types::TypeKind::Ptr) {
                        Value ptrReg = Register { .name = dstName,
                            .type = reg_.getPrimitive(types::TypeKind::Ptr),
                            .pos = s->pos };
                        Value writeVal;
                        if (s->op != TokenType::ASSIGN) {
                            writeVal = emitCompoundMath(
                                ptrReg, s->op, s->rValue, ident->exprType, s->pos);
                        } else {
                            writeVal = lowerExpr(s->rValue);
                        }
                        push(StoreInst { .dstPtr = ptrReg,
                            .value = writeVal,
                            .type = ident->exprType,
                            .pos = s->pos });
                        return;
                    }

                    // Standard assignment fallback for value types
                    locals_[dstName] = ident->exprType;
                    Value writeVal;
                    if (s->op != TokenType::ASSIGN) {
                        Value flatLHS = lowerExpr(s->lValue);
                        Value flatRHS = lowerExpr(s->rValue);
                        SymID opTmp = newTemp();
                        writeVal = emit(BinaryOpInst { .dst = opTmp,
                                            .left = flatLHS,
                                            .op = s->op,
                                            .right = flatRHS,
                                            .type = ident->exprType,
                                            .pos = s->pos },
                            opTmp, ident->exprType);
                    } else {
                        writeVal = lowerExpr(s->rValue);
                    }
                    emitTransfer(dstName, writeVal, s->pos);
                }
            },
            [&](ast::ExprStmt* s) { lowerExpr(s->value); },
            [&](ast::YieldStmt* s) { lowerExpr(s->value); },
            [&](ast::BreakStmt* s) {
                if (!loops_.empty()) {
                    current_->terminator
                        = JumpTerminator { .target = loops_.back().exit, .pos = s->pos };
                    current_ = nullptr;
                }
            },
            [&](ast::ContinueStmt* s) {
                if (!loops_.empty()) {
                    current_->terminator
                        = JumpTerminator { .target = loops_.back().header, .pos = s->pos };
                    current_ = nullptr;
                }
            },
            [&](ast::ReturnStmt* s) {
                Value flatRet = std::monostate {};
                if (!std::holds_alternative<std::monostate>(s->value)) {
                    flatRet = lowerExpr(s->value);
                    const types::Type* retType = getTypeOf(flatRet);
                    if (retType && retType->kind == types::TypeKind::Unit) {
                        flatRet = std::monostate {};
                    }
                }

                if (!std::holds_alternative<std::monostate>(currentFuture_)) {
                    BasicBlock* suspendBlock = newBlock();
                    BasicBlock* cleanupBlock = newBlock();

                    current_->terminator = CoroFinalSuspendTerminator { .value = flatRet,
                        .suspendBlock = suspendBlock->id,
                        .cleanupBlock = cleanupBlock->id,
                        .pos = s->pos };
                    cleanupBlock->terminator = CoroYieldTerminator { s->pos };
                    suspendBlock->terminator = UnreachableTerminator { s->pos };
                } else {
                    current_->terminator = ReturnTerminator { .value = flatRet, .pos = s->pos };
                }
                current_ = nullptr;
            },
            [&](ast::ForStmt* s) {
                BasicBlock* condBlock = newBlock();
                BasicBlock* bodyBlock = newBlock();
                BasicBlock* postBlock = newBlock();
                BasicBlock* exitBlock = newBlock();

                if (!std::holds_alternative<std::monostate>(s->init))
                    lowerStmt(s->init);

                if (current_) {
                    current_->terminator
                        = JumpTerminator { .target = condBlock->id, .pos = s->pos };
                }

                current_ = condBlock;
                Value flatCond;
                if (!std::holds_alternative<std::monostate>(s->condition)) {
                    flatCond = lowerExpr(s->condition);
                } else {
                    flatCond = BoolConstant { .value = true,
                        .type = reg_.getPrimitive(types::TypeKind::Bool),
                        .pos = s->pos };
                }

                current_->terminator = BranchTerminator { .condition = flatCond,
                    .trueTarget = bodyBlock->id,
                    .falseTarget = exitBlock->id,
                    .pos = s->pos };

                loops_.push_back({ postBlock->id, exitBlock->id });

                current_ = bodyBlock;
                if (s->body)
                    lowerStmt(s->body);
                if (current_ && std::holds_alternative<std::monostate>(current_->terminator)) {
                    current_->terminator
                        = JumpTerminator { .target = postBlock->id, .pos = s->pos };
                }

                current_ = postBlock;
                if (!std::holds_alternative<std::monostate>(s->post))
                    lowerStmt(s->post);
                if (current_ && std::holds_alternative<std::monostate>(current_->terminator)) {
                    current_->terminator
                        = JumpTerminator { .target = condBlock->id, .pos = s->pos };
                }

                loops_.pop_back();
                current_ = exitBlock;
            },
            [&](ast::VecPushStmt* s) {
                Value vecPtr = addressOf(s->lValue);
                const types::Type* vecType = safeGetExprType(s->lValue);
                const types::Type* elemType = nullptr;
                if (vecType && vecType->kind == types::TypeKind::Vector) {
                    elemType = std::get<types::VectorPayload>(vecType->payload).base;
                }

                Value flatElem = lowerExpr(s->rValue);

                if (!elemType) {
                    elemType = getTypeOf(flatElem);
                }
                if (!elemType) {
                    elemType = safeGetExprType(s->rValue);
                }

                Value boxedElem;
                if (elemType && isAggregateType(elemType)) {
                    if (auto* reg = std::get_if<Register>(&flatElem)) {
                        boxedElem = emitBorrow(reg->name, true, s->pos);
                    } else {
                        boxedElem = boxScalar(flatElem, elemType, s->pos);
                    }
                } else {
                    boxedElem = boxScalar(flatElem, elemType, s->pos);
                }
                EmitMamlVecPush(vecPtr, boxedElem, s->pos);
            },
        },
        stmt);
}

// =============================================================================
// Address Resolution (L-Values)
// =============================================================================

Value Builder::addressOf(ast::Expr expr)
{
    return std::visit(
        overloaded { [&](std::monostate) -> Value { return std::monostate {}; },
            [&](ast::Identifier* e) -> Value {
                SymID regName = resolveLocal(e->name);
                // if (locals_[regName]->kind == types::TypeKind::Ptr) {
                //     return Register { .name = regName,
                //         .type = reg_.getPrimitive(types::TypeKind::Ptr),
                //         .pos = e->pos };
                // }
                return emitBorrow(regName, true, e->pos);
            },
            [&](ast::FieldAccess* e) -> Value {
                Value basePtr = addressOf(e->object);
                const types::Type* objType = safeGetExprType(e->object);

                int fieldIndex = -1;
                if (objType && objType->kind == types::TypeKind::Struct) {
                    const auto& fields = std::get<types::StructPayload>(objType->payload).fields;
                    for (size_t i = 0; i < fields.size(); ++i) {
                        if (fields[i].name == e->field->name) {
                            fieldIndex = static_cast<int>(i);
                            break;
                        }
                    }
                }
                return emitFieldAddr(
                    basePtr, objType, e->field->name, fieldIndex, e->exprType, e->pos);
            },
            [&](ast::IndexExpr* e) -> Value {
                Value basePtr = addressOf(e->left);
                const types::Type* sourceType = safeGetExprType(e->left);

                if (sourceType) {
                    if (sourceType->kind == types::TypeKind::String
                        || sourceType->kind == types::TypeKind::View) {
                        Value rawDataPtr = loadField(basePtr, sourceType, sym_.intern("ptr"), 0,
                            reg_.getPrimitive(types::TypeKind::Ptr), e->pos);

                        const types::Type* elemType = reg_.getPrimitive(types::TypeKind::U8);
                        if (sourceType->kind == types::TypeKind::View) {
                            elemType = std::get<types::ViewPayload>(sourceType->payload).base;
                        }

                        Value idxVal = lowerExpr(e->index);
                        SymID elemPtrTmp = newPtrTemp();

                        return emit(
                            IndexAddrInst { elemPtrTmp, rawDataPtr,
                                reg_.getPrimitive(types::TypeKind::Ptr), idxVal, elemType, e->pos },
                            elemPtrTmp, reg_.getPrimitive(types::TypeKind::Ptr));
                    } else if (sourceType->kind == types::TypeKind::Vector) {
                        Value flatIdx = lowerExpr(e->index);
                        return EmitMamlVecGet(basePtr, flatIdx, e->pos);
                    }
                }

                Value idxVal = lowerExpr(e->index);
                SymID ptrTmp = newPtrTemp();
                return emit(
                    IndexAddrInst { ptrTmp, basePtr, sourceType, idxVal, e->exprType, e->pos },
                    ptrTmp, reg_.getPrimitive(types::TypeKind::Ptr));
            },
            [&](ast::TaggedUnionAccessExpr* e) -> Value {
                Value objPtr = addressOf(e->object);
                const types::Type* layoutType = safeGetExprType(e->object);

                Value rawPayloadAddr = emitFieldAddr(objPtr, layoutType, sym_.intern("payload"), 1,
                    reg_.getPrimitive(types::TypeKind::Unknown), e->pos);

                SymID castTmp = newPtrTemp();
                push(BitcastPtrInst { .dst = castTmp,
                    .src = rawPayloadAddr,
                    .type = reg_.getPrimitive(types::TypeKind::Ptr),
                    .pos = e->pos });
                Value typedPayloadPtr = Register {
                    .name = castTmp, .type = reg_.getPrimitive(types::TypeKind::Ptr), .pos = e->pos
                };

                SymID fieldName = sym_.intern("payload_" + std::to_string(e->fieldIndex));
                const types::Type* fieldType = e->exprType;
                if (e->payloadStructType && e->payloadStructType->kind == types::TypeKind::Struct) {
                    const auto& fields
                        = std::get<types::StructPayload>(e->payloadStructType->payload).fields;
                    if (static_cast<size_t>(e->fieldIndex) < fields.size()) {
                        fieldName = fields[e->fieldIndex].name;
                        if (fields[e->fieldIndex].type) {
                            fieldType = fields[e->fieldIndex].type;
                        }
                    }
                }

                // Return the memory address of the field instead of loading it
                return emitFieldAddr(typedPayloadPtr, e->payloadStructType, fieldName,
                    e->fieldIndex, fieldType, e->pos);
            },
            [&](auto) -> Value {
                // Should not be reached for valid L-Values
                return std::monostate {};
            } },
        expr);
}

// =============================================================================
// Helper Methods for Expressions
// =============================================================================

Value Builder::emitGetFutureResult(Value futureVal, const types::Type* resultType, Position pos)
{
    if (resultType->kind == types::TypeKind::Unit) {
        return std::monostate {};
    }

    SymID promisePtr = newTemp();
    locals_[promisePtr] = reg_.getPrimitive(types::TypeKind::Ptr);
    push(
        CoroPromisePtrInst { promisePtr, futureVal, reg_.getPrimitive(types::TypeKind::Ptr), pos });

    SymID res = emitTemp(resultType);
    push(LoadPtrInst { res, Register { promisePtr, reg_.getPrimitive(types::TypeKind::Ptr), pos },
        resultType, pos });
    return Register { res, resultType, pos };
}

Value Builder::flattenStringEq(ast::InfixExpr* e, Value flatLeft, Value flatRight)
{
    const types::Type* strTy = reg_.getPrimitive(types::TypeKind::String);

    SymID leftTmp = emitTemp(strTy);
    push(AllocaInst { leftTmp, strTy, e->pos });
    emitTransfer(leftTmp, flatLeft, e->pos);

    SymID rightTmp = emitTemp(strTy);
    push(AllocaInst { rightTmp, strTy, e->pos });
    emitTransfer(rightTmp, flatRight, e->pos);

    Value leftPtr = emitBorrow(leftTmp, false, e->pos);
    Value rightPtr = emitBorrow(rightTmp, false, e->pos);

    Value callVal = emitRuntimeCall(sym_.intern("maml_str_eq"),
        reg_.getPrimitive(types::TypeKind::I32), { leftPtr, rightPtr }, e->pos);

    SymID boolTmp = newTemp();
    Value result = emit(BinaryOpInst { boolTmp, callVal, TokenType::NOT_EQ,
                            IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), e->pos },
                            reg_.getPrimitive(types::TypeKind::Bool), e->pos },
        boolTmp, reg_.getPrimitive(types::TypeKind::Bool));

    if (e->op == TokenType::NOT_EQ) {
        SymID notTmp = newTemp();
        result = emit(UnaryOpInst { notTmp, TokenType::NOT, result,
                          reg_.getPrimitive(types::TypeKind::Bool), e->pos },
            notTmp, reg_.getPrimitive(types::TypeKind::Bool));
    }

    return result;
}

std::tuple<Value, Value, Value> Builder::lowerMapKey(ast::Expr keyExpr, Position pos)
{
    Value flatKey = lowerExpr(keyExpr);
    const types::Type* keyType = safeGetExprType(keyExpr);

    if (keyType->kind == types::TypeKind::I64 || keyType->kind == types::TypeKind::U64) {
        SymID hashTmp = newTemp();
        Value hashVal
            = emit(CastInst { hashTmp, flatKey, reg_.getPrimitive(types::TypeKind::I64), pos },
                hashTmp, reg_.getPrimitive(types::TypeKind::I64));
        Value ptrVal = IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos };
        Value lenVal = IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos };
        return { hashVal, ptrVal, lenVal };
    } else if (keyType->kind == types::TypeKind::String) {
        SymID keyTmp = emitTemp(keyType);
        push(AllocaInst { keyTmp, keyType, pos }); // FIX: Stack allocate temporary key
        emitTransfer(keyTmp, flatKey, pos);
        Value safeKey = Register { keyTmp, keyType, pos };

        Value ptrVal = loadField(
            safeKey, keyType, sym_.intern("ptr"), 0, reg_.getPrimitive(types::TypeKind::Ptr), pos);
        Value lenVal = loadField(
            safeKey, keyType, sym_.intern("len"), 1, reg_.getPrimitive(types::TypeKind::U32), pos);
        Value strPtr = emitBorrow(keyTmp, false, pos);

        Value hashVal = emitRuntimeCall(
            sym_.intern("maml_str_hash"), reg_.getPrimitive(types::TypeKind::U32), { strPtr }, pos);
        return { hashVal, ptrVal, lenVal };
    }

    return { IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos },
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos },
        IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos } };
}

// =============================================================================
// Expression Lowering
// =============================================================================

Value Builder::lowerExpr(ast::Expr expr)
{
    if (!current_)
        return std::monostate {};

    return std::visit(
        overloaded { [&](std::monostate) -> Value { return std::monostate {}; },
            [&](ast::BlockStmt* s) -> Value {
                lowerBlockStmt(s);

                // A block used as an expression yields the value of its trailing
                // YieldStmt/ExprStmt, mirroring how IfExpr extracts branch results.
                if (!current_ || !s || s->statements.empty()) {
                    return std::monostate {};
                }

                return std::visit(
                    [this](auto&& tailStmt) -> Value {
                        using T = std::decay_t<decltype(tailStmt)>;
                        if constexpr (std::is_same_v<T, ast::YieldStmt*>) {
                            return lowerExpr(tailStmt->value);
                        } else if constexpr (std::is_same_v<T, ast::ExprStmt*>) {
                            return lowerExpr(tailStmt->value);
                        } else {
                            return std::monostate {};
                        }
                    },
                    s->statements.back());
            },
            [&](ast::Identifier* e) -> Value {
                // 1. Check if it's a local variable in the environment
                SymID regName = resolveLocal(e->name);
                if (locals_.find(regName) != locals_.end()) {
                    const types::Type* t = locals_[regName];

                    if (t && e->exprType && t->kind == types::TypeKind::Ptr
                        && e->exprType->kind != types::TypeKind::Ptr) {
                        Value ptrReg = Register { .name = regName, .type = t, .pos = e->pos };
                        return emitLoad(ptrReg, e->exprType, e->pos);
                    }
                    return Register { .name = regName, .type = t, .pos = e->pos };
                }

                // REMOVED: Fallback loop checking for unit Sum Type constructors (None, Red, etc.)

                return Register { .name = regName,
                    .type = reg_.getPrimitive(types::TypeKind::Unknown),
                    .pos = e->pos };
            },
            [&](ast::IntLiteral* e) -> Value {
                const types::Type* ty
                    = e->exprType ? e->exprType : reg_.getPrimitive(types::TypeKind::I64);
                return IntConstant { .value = e->value, .type = ty, .pos = e->pos };
            },
            [&](ast::BoolLiteral* e) -> Value {
                const types::Type* ty
                    = e->exprType ? e->exprType : reg_.getPrimitive(types::TypeKind::Bool);
                return BoolConstant { .value = e->value, .type = ty, .pos = e->pos };
            },
            [&](ast::StringLiteral* e) -> Value {
                SymID tmp = emitTemp(e->exprType);
                push(AllocaInst { .dst = tmp, .type = e->exprType, .pos = e->pos });
                Value obj = Register { .name = tmp, .type = e->exprType, .pos = e->pos };
                Value rawStrPtr = StringConstant { .value = e->value,
                    .type = reg_.getPrimitive(types::TypeKind::Ptr),
                    .pos = e->pos };

                storeField(obj, e->exprType, rawStrPtr, sym_.intern("ptr"), 0,
                    reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                storeField(obj, e->exprType,
                    IntConstant { .value = static_cast<int64_t>(e->value.length()),
                        .type = reg_.getPrimitive(types::TypeKind::I32),
                        .pos = e->pos },
                    sym_.intern("len"), 1, reg_.getPrimitive(types::TypeKind::I32), e->pos);
                storeField(obj, e->exprType,
                    BoolConstant { false, reg_.getPrimitive(types::TypeKind::Bool), e->pos },
                    sym_.intern("is_owned"), 2, reg_.getPrimitive(types::TypeKind::Bool), e->pos);
                return obj;
            },
            [&](ast::InfixExpr* e) -> Value {
                // REMOVED: Logical short-circuiting for && and || (now lowered to IfExpr by
                // hir::Desugarer)

                Value flatLeft = lowerExpr(e->left);
                Value flatRight = lowerExpr(e->right);

                const types::Type* lType = getTypeOf(flatLeft);
                if (lType && lType->kind == types::TypeKind::String
                    && (e->op == TokenType::EQ || e->op == TokenType::NOT_EQ)) {
                    return flattenStringEq(e, flatLeft, flatRight);
                }

                SymID tmp = newTemp();
                return emit(BinaryOpInst { tmp, flatLeft, e->op, flatRight, e->exprType, e->pos },
                    tmp, e->exprType);
            },
            [&](ast::PrefixExpr* e) -> Value {
                Value flatRight = lowerExpr(e->right);
                SymID tmp = newTemp();
                return emit(
                    UnaryOpInst { tmp, e->op, flatRight, e->exprType, e->pos }, tmp, e->exprType);
            },
            [&](ast::IfExpr* e) -> Value {
                Value flatCond = lowerExpr(e->condition);

                BasicBlock* thenBlock = newBlock();
                BasicBlock* mergeBlock = newBlock();
                BasicBlock* elseBlock
                    = (e->alternative || e->alternativeIsUnreachable) ? newBlock() : mergeBlock;

                bool isUnit = (e->exprType->kind == types::TypeKind::Unit);
                SymID resultTemp = NoSymbol;
                Value resultReg = std::monostate {};

                if (!isUnit) {
                    resultTemp = emitTemp(e->exprType);
                    push(AllocaInst { resultTemp, e->exprType, e->pos });
                    resultReg = Register { resultTemp, e->exprType, e->pos };
                }

                current_->terminator
                    = BranchTerminator { flatCond, thenBlock->id, elseBlock->id, e->pos };

                // Then Branch
                current_ = thenBlock;
                if (e->consequence)
                    lowerBlockStmt(e->consequence);
                bool mergeReachable = false;
                if (current_) {
                    if (!isUnit) {
                        Value thenYield = lowerExpr(e->consequence->statements.empty()
                                ? ast::Expr(std::monostate {})
                                : std::visit(
                                      [](auto&& s) -> ast::Expr {
                                          using T = std::decay_t<decltype(s)>;
                                          if constexpr (std::is_same_v<T, ast::YieldStmt*>) {
                                              return s->value;
                                          }
                                          return std::monostate {};
                                      },
                                      e->consequence->statements.back()));
                        if (!std::holds_alternative<std::monostate>(thenYield))
                            push(AssignInst {
                                .dst = resultTemp, .rValue = thenYield, .pos = e->pos });
                    }
                    if (std::holds_alternative<std::monostate>(current_->terminator))
                        current_->terminator
                            = JumpTerminator { .target = mergeBlock->id, .pos = e->pos };
                    mergeReachable = true;
                }

                // Else Branch
                if (e->alternative) {
                    current_ = elseBlock;
                    lowerBlockStmt(e->alternative);
                    if (current_) {
                        if (!isUnit) {
                            Value elseYield = lowerExpr(e->alternative->statements.empty()
                                    ? ast::Expr(std::monostate {})
                                    : std::visit(
                                          [](auto&& s) -> ast::Expr {
                                              using T = std::decay_t<decltype(s)>;
                                              if constexpr (std::is_same_v<T, ast::YieldStmt*>) {
                                                  return s->value;
                                              }
                                              return std::monostate {};
                                          },
                                          e->alternative->statements.back()));
                            if (!std::holds_alternative<std::monostate>(elseYield))
                                push(AssignInst { resultTemp, elseYield, e->pos });
                        }
                        if (std::holds_alternative<std::monostate>(current_->terminator))
                            current_->terminator = JumpTerminator { mergeBlock->id, e->pos };
                        mergeReachable = true;
                    }
                } else if (e->alternativeIsUnreachable) {
                    // Exhaustive match, no wildcard arm: this branch is provably impossible.
                    // Terminate here instead of silently falling through into mergeBlock.
                    elseBlock->terminator = UnreachableTerminator { e->pos };
                    // Contributes nothing reachable — do not set mergeReachable from this path.
                } else {
                    mergeReachable = true;
                }

                if (mergeReachable)
                    current_ = mergeBlock;
                else {
                    current_ = nullptr;
                    graph_->blocks.erase(mergeBlock->id); // Unreachable
                }

                return resultReg;
            },
            [&](ast::CallExpr* e) -> Value {
                // REMOVED: String-matching intrinsic checks ("yield_now", "len", "delete")
                // REMOVED: Sum Type constructor variant loop

                // Keep run_executor runtime dispatch if needed by async top-level setup
                if (auto** idPtr = std::get_if<ast::Identifier*>(&e->function)) {
                    std::string_view fnName = sym_.resolve((*idPtr)->name);
                    if (fnName == "run_executor") {
                        Value flatArg = lowerExpr(e->arguments[0].argument);
                        emitRuntimeCall(sym_.intern("maml_run_executor"),
                            reg_.getPrimitive(types::TypeKind::Ptr), { flatArg }, e->pos);
                        return emitGetFutureResult(flatArg, e->exprType, e->pos);
                    }
                }

                Value flatFunc = lowerExpr(e->function);
                std::vector<Value> flatArgs;
                std::vector<bool> argConsumed;

                for (const auto& arg : e->arguments) {
                    const types::Type* argType = safeGetExprType(arg.argument);
                    const types::Type* resultType = lowerParamType(argType, arg.cap);

                    SymID argTmp = emitTemp(resultType);
                    if (arg.cap == ast::Capability::Mut) {
                        Value ptrVal = addressOf(arg.argument);
                        push(AssignInst { argTmp, ptrVal, arg.pos });
                    } else {
                        Value flatArg = lowerExpr(arg.argument);
                        if (auto* srcReg = std::get_if<Register>(&flatArg)) {
                            emitCapTransfer(argTmp, srcReg->name, arg.cap, arg.pos);
                        } else {
                            push(AssignInst { argTmp, flatArg, arg.pos });
                        }
                    }
                    flatArgs.push_back(Register { argTmp, resultType, arg.pos });
                    argConsumed.push_back(arg.cap == ast::Capability::Own);
                }

                SymID tmp = emitTemp(e->exprType);
                return emit(CallInst { tmp, flatFunc, flatArgs, argConsumed, e->exprType, e->pos },
                    tmp, e->exprType);
            },
            [&](ast::FieldAccess* e) -> Value {
                Value ptrVal = addressOf(e);
                return emitLoad(ptrVal, e->exprType, e->pos);
            },
            [&](ast::IndexExpr* e) -> Value {
                Value ptrVal = addressOf(e);
                const types::Type* sourceType = safeGetExprType(e->left);

                // Note: Map Option wrapping can remain here unless desugared separately
                if (sourceType->kind == types::TypeKind::Map) {
                    auto [hashVal, keyPtrVal, lenVal] = lowerMapKey(e->index, e->pos);
                    Value opaquePtr = emitRuntimeCall(sym_.intern("maml_map_get"),
                        reg_.getPrimitive(types::TypeKind::Ptr),
                        { ptrVal, hashVal, keyPtrVal, lenVal }, e->pos);

                    SymID resTmp = emitTemp(e->exprType);
                    push(AllocaInst { .dst = resTmp, .type = e->exprType, .pos = e->pos });
                    SymID cmpTmp = newTemp();
                    push(BinaryOpInst { .dst = cmpTmp,
                        .left = opaquePtr,
                        .op = TokenType::NOT_EQ,
                        .right = IntConstant { .value = 0,
                            .type = reg_.getPrimitive(types::TypeKind::I64),
                            .pos = e->pos },
                        .type = reg_.getPrimitive(types::TypeKind::Bool),
                        .pos = e->pos });

                    BasicBlock* thenBlock = newBlock();
                    BasicBlock* elseBlock = newBlock();
                    BasicBlock* mergeBlock = newBlock();

                    current_->terminator
                        = BranchTerminator { .condition = Register { .name = cmpTmp,
                                                 .type = reg_.getPrimitive(types::TypeKind::Bool),
                                                 .pos = e->pos },
                              .trueTarget = thenBlock->id,
                              .falseTarget = elseBlock->id,
                              .pos = e->pos };

                    const types::Type* valType
                        = std::get<types::SumPayload>(e->exprType->payload).typeArgs[0];

                    current_ = thenBlock;
                    SymID valTmp = emitTemp(valType);
                    push(LoadPtrInst { valTmp, opaquePtr, valType, e->pos });
                    // Explicit store to structural Option layout instead of emitVariantInit
                    SymID someTmp = emitTemp(e->exprType);
                    push(AllocaInst { .dst = someTmp, .type = e->exprType, .pos = e->pos });
                    Value somePtr = emitBorrow(someTmp, true, e->pos);
                    storeField(somePtr, e->exprType,
                        IntConstant { 0, reg_.getPrimitive(types::TypeKind::I32), e->pos },
                        sym_.intern("discriminant"), 0, reg_.getPrimitive(types::TypeKind::I32),
                        e->pos);
                    storeField(somePtr, e->exprType, Register { valTmp, valType, e->pos },
                        sym_.intern("payload_0"), 1, valType, e->pos);

                    push(AssignInst { resTmp, Register { someTmp, e->exprType, e->pos }, e->pos });
                    thenBlock->terminator = JumpTerminator { mergeBlock->id, e->pos };

                    current_ = elseBlock;
                    SymID noneTmp = emitTemp(e->exprType);
                    push(AllocaInst { .dst = noneTmp, .type = e->exprType, .pos = e->pos });
                    Value nonePtr = emitBorrow(noneTmp, true, e->pos);
                    storeField(nonePtr, e->exprType,
                        IntConstant { 1, reg_.getPrimitive(types::TypeKind::I32), e->pos },
                        sym_.intern("discriminant"), 0, reg_.getPrimitive(types::TypeKind::I32),
                        e->pos);

                    push(AssignInst { resTmp, Register { noneTmp, e->exprType, e->pos }, e->pos });
                    elseBlock->terminator = JumpTerminator { mergeBlock->id, e->pos };

                    if (auto* reg = std::get_if<Register>(&ptrVal))
                        push(KeepAliveInst { reg->name, e->pos });
                    current_ = mergeBlock;
                    return Register { resTmp, e->exprType, e->pos };
                }

                Value rawVal = emitLoad(ptrVal, e->exprType, e->pos);
                if (sourceType->kind == types::TypeKind::String) {
                    SymID castTmp = newTemp();
                    return emit(
                        CastInst { castTmp, rawVal, e->exprType, e->pos }, castTmp, e->exprType);
                }
                return rawVal;
            },
            [&](ast::SliceExpr* e) -> Value {
                Value basePtr = addressOf(e->left);
                const types::Type* sourceType = safeGetExprType(e->left);

                Value origPtr, origLen;
                const types::Type* elemType = nullptr;

                if (sourceType->kind == types::TypeKind::String) {
                    origPtr = loadField(basePtr, sourceType, sym_.intern("ptr"), 0,
                        reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                    origLen = loadField(basePtr, sourceType, sym_.intern("len"), 1,
                        reg_.getPrimitive(types::TypeKind::I64), e->pos);
                    elemType = reg_.getPrimitive(types::TypeKind::U8);
                } else if (sourceType->kind == types::TypeKind::View) {
                    origPtr = loadField(basePtr, sourceType, sym_.intern("ptr"), 0,
                        reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                    origLen = loadField(basePtr, sourceType, sym_.intern("len"), 1,
                        reg_.getPrimitive(types::TypeKind::I64), e->pos);
                    elemType = std::get<types::ViewPayload>(sourceType->payload).base;
                } else if (sourceType->kind == types::TypeKind::Vector) {
                    origPtr = loadField(basePtr, sourceType, sym_.intern("buffer"), 0,
                        reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                    origLen = loadField(basePtr, sourceType, sym_.intern("len"), 2,
                        reg_.getPrimitive(types::TypeKind::I64), e->pos);
                    elemType = std::get<types::VectorPayload>(sourceType->payload).base;
                } else if (sourceType->kind == types::TypeKind::Array) {
                    origPtr = basePtr;
                    origLen = IntConstant { std::get<types::ArrayPayload>(sourceType->payload).size,
                        reg_.getPrimitive(types::TypeKind::I64), e->pos };
                    elemType = std::get<types::ArrayPayload>(sourceType->payload).base;
                }

                Value lowVal = !std::holds_alternative<std::monostate>(e->low)
                    ? lowerExpr(e->low)
                    : IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), e->pos };
                Value highVal = !std::holds_alternative<std::monostate>(e->high)
                    ? lowerExpr(e->high)
                    : origLen;

                SymID newLenTmp = newTemp();
                Value newLen = emit(BinaryOpInst { newLenTmp, highVal, TokenType::MINUS, lowVal,
                                        reg_.getPrimitive(types::TypeKind::I64), e->pos },
                    newLenTmp, reg_.getPrimitive(types::TypeKind::I64));

                SymID newDataPtrTmp = newTemp();
                Value newDataPtr
                    = emit(IndexAddrInst { newDataPtrTmp, origPtr,
                               reg_.getPrimitive(types::TypeKind::Ptr), lowVal, elemType, e->pos },
                        newDataPtrTmp, reg_.getPrimitive(types::TypeKind::Ptr));

                SymID resultTmp = emitTemp(e->exprType);
                push(AllocaInst { resultTmp, e->exprType, e->pos });
                Value result = Register { resultTmp, e->exprType, e->pos };

                storeField(result, e->exprType, newDataPtr, sym_.intern("ptr"), 0,
                    reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                storeField(result, e->exprType, newLen, sym_.intern("len"), 1,
                    reg_.getPrimitive(types::TypeKind::I64), e->pos);

                if (e->exprType->kind == types::TypeKind::String) {
                    storeField(result, e->exprType,
                        BoolConstant { false, reg_.getPrimitive(types::TypeKind::Bool), e->pos },
                        sym_.intern("is_owned"), 2, reg_.getPrimitive(types::TypeKind::Bool),
                        e->pos);
                }
                return result;
            },
            [&](ast::AwaitExpr* e) -> Value {
                Value flatTask = lowerExpr(e->value);
                emitRuntimeCall(sym_.intern("maml_task_await"),
                    reg_.getPrimitive(types::TypeKind::Unit), { flatTask, currentFuture_ }, e->pos);
                current_ = emitCoroSuspend(e->pos);
                return emitGetFutureResult(flatTask, e->exprType, e->pos);
            },
            [&](ast::SpawnExpr* e) -> Value {
                Value flatFuture = lowerExpr(e->value);
                emitRuntimeCall(sym_.intern("maml_spawn_task"),
                    reg_.getPrimitive(types::TypeKind::Unit), { flatFuture }, e->pos);
                return flatFuture;
            },
            [&](ast::CompositeLiteral* e) -> Value {
                const types::Type* t = e->exprType;
                if (!t)
                    return std::monostate {};

                if (t->kind == types::TypeKind::Vector) {
                    return lowerVecLiteral(e, t);
                }
                if (t->kind == types::TypeKind::Map) {
                    return lowerMapLiteral(e, t);
                }
                if (t->kind == types::TypeKind::Struct) {
                    const auto& fields = std::get<types::StructPayload>(t->payload).fields;
                    SymID tmp = emitTemp(t);
                    push(AllocaInst { .dst = tmp, .type = t, .pos = e->pos });
                    Value obj = Register { .name = tmp, .type = t, .pos = e->pos };

                    for (size_t i = 0; i < e->elements.size(); ++i) {
                        const auto& elem = e->elements[i];
                        int fieldIdx = -1;
                        SymID fieldName = NoSymbol;
                        const types::Type* fieldType = nullptr;

                        if (!std::holds_alternative<std::monostate>(elem.key)) {
                            if (auto idPtr = std::get_if<ast::Identifier*>(&elem.key)) {
                                fieldName = (*idPtr)->name;
                                for (size_t j = 0; j < fields.size(); ++j) {
                                    if (fields[j].name == fieldName) {
                                        fieldIdx = static_cast<int>(j);
                                        fieldType = fields[j].type;
                                        break;
                                    }
                                }
                            }
                        } else if (i < fields.size()) {
                            fieldIdx = static_cast<int>(i);
                            fieldName = fields[i].name;
                            fieldType = fields[i].type;
                        }

                        if (fieldIdx >= 0 && fieldType) {
                            Value flatVal = lowerExpr(elem.value);
                            storeField(obj, t, flatVal, fieldName, fieldIdx, fieldType, elem.pos);
                        }
                    }
                    return obj;
                }
                if (t->kind == types::TypeKind::Array) {
                    const auto& payload = std::get<types::ArrayPayload>(t->payload);
                    const types::Type* elemType = payload.base;
                    size_t arraySize = payload.size;

                    SymID tmp = emitTemp(t);
                    push(AllocaInst { .dst = tmp, .type = t, .pos = e->pos });
                    Value arrPtr = emitBorrow(tmp, true, e->pos);

                    for (size_t i = 0; i < e->elements.size() && i < arraySize; ++i) {
                        Value flatElem = lowerExpr(e->elements[i].value);
                        Value idx = IntConstant { .value = static_cast<int64_t>(i),
                            .type = reg_.getPrimitive(types::TypeKind::I64),
                            .pos = e->elements[i].pos };

                        SymID elemAddrTmp = newPtrTemp();
                        Value elemAddr = emit(IndexAddrInst { .dst = elemAddrTmp,
                                                  .source = arrPtr,
                                                  .sourceType = t,
                                                  .index = idx,
                                                  .type = elemType,
                                                  .pos = e->elements[i].pos },
                            elemAddrTmp, reg_.getPrimitive(types::TypeKind::Ptr));

                        push(StoreInst { elemAddr, flatElem, elemType, e->elements[i].pos });
                    }
                    return Register { tmp, t, e->pos };
                }

                return std::monostate {};
            },
            [&](ast::TaggedUnionConstructExpr* e) -> Value {
                // Step 1: Allocate stack space for the layout struct (discriminant + payload array)
                const types::Type* layoutType = e->exprType;
                SymID tmp = emitTemp(layoutType);
                push(AllocaInst { .dst = tmp, .type = layoutType, .pos = e->pos });
                Value basePtr = emitBorrow(tmp, true, e->pos);

                // Step 2: Store the discriminant integer into Field 0 (i32)
                const types::Type* i32Type = reg_.getPrimitive(types::TypeKind::I32);
                Value discVal
                    = IntConstant { .value = e->discriminant, .type = i32Type, .pos = e->pos };
                storeField(
                    basePtr, layoutType, discVal, sym_.intern("discriminant"), 0, i32Type, e->pos);

                // Step 3: If this variant has payload arguments, store them into Field 1
                if (!e->payloadArgs.empty() && e->payloadStructType) {
                    Value rawPayloadAddr
                        = emitFieldAddr(basePtr, layoutType, sym_.intern("payload"), 1,
                            reg_.getPrimitive(types::TypeKind::Unknown), e->pos);

                    // Bitcast the raw byte array pointer to the variant's structural payload struct
                    SymID castTmp = newPtrTemp();
                    push(BitcastPtrInst { .dst = castTmp,
                        .src = rawPayloadAddr,
                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                        .pos = e->pos });
                    Value typedPayloadPtr = Register { .name = castTmp,
                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                        .pos = e->pos };

                    // Step 4: Evaluate each payload argument and store it into the bitcasted struct
                    const auto& payloadFields
                        = std::get<types::StructPayload>(e->payloadStructType->payload).fields;
                    for (size_t i = 0; i < e->payloadArgs.size(); ++i) {
                        Value argValue = lowerExpr(e->payloadArgs[i]);
                        const types::Type* fieldType = (i < payloadFields.size())
                            ? payloadFields[i].type
                            : getTypeOf(argValue);
                        SymID fieldName = (i < payloadFields.size())
                            ? payloadFields[i].name
                            : sym_.intern("payload_" + std::to_string(i));

                        storeField(typedPayloadPtr, e->payloadStructType, argValue, fieldName,
                            static_cast<int>(i), fieldType, e->pos);
                    }
                }

                return Register { .name = tmp, .type = layoutType, .pos = e->pos };
            },
            [&](ast::TaggedUnionAccessExpr* e) -> Value {
                // Step 1: Get the pointer address of the underlying layout struct
                Value objPtr = addressOf(e->object);
                const types::Type* layoutType = safeGetExprType(e->object);

                // Step 2: Address Field 1 (payload byte array)
                Value rawPayloadAddr = emitFieldAddr(objPtr, layoutType, sym_.intern("payload"), 1,
                    reg_.getPrimitive(types::TypeKind::Unknown), e->pos);

                // Step 3: Bitcast raw byte array pointer to the payload struct pointer
                SymID castTmp = newPtrTemp();
                push(BitcastPtrInst { .dst = castTmp,
                    .src = rawPayloadAddr,
                    .type = reg_.getPrimitive(types::TypeKind::Ptr),
                    .pos = e->pos });
                Value typedPayloadPtr = Register {
                    .name = castTmp, .type = reg_.getPrimitive(types::TypeKind::Ptr), .pos = e->pos
                };

                // Step 4: Resolve field name/type and load from the bitcasted payload struct
                SymID fieldName = sym_.intern("payload_" + std::to_string(e->fieldIndex));
                const types::Type* fieldType = e->exprType;
                if (e->payloadStructType && e->payloadStructType->kind == types::TypeKind::Struct) {
                    const auto& fields
                        = std::get<types::StructPayload>(e->payloadStructType->payload).fields;
                    if (static_cast<size_t>(e->fieldIndex) < fields.size()) {
                        fieldName = fields[e->fieldIndex].name;
                        if (fields[e->fieldIndex].type) {
                            fieldType = fields[e->fieldIndex].type;
                        }
                    }
                }

                return loadField(typedPayloadPtr, e->payloadStructType, fieldName, e->fieldIndex,
                    fieldType, e->pos);
            },
            [&](ast::CastExpr* e) -> Value {
                Value flatSrc = lowerExpr(e->source);
                SymID castTmp = emitTemp(e->targetType);
                return emit(
                    CastInst {
                        .dst = castTmp, .src = flatSrc, .type = e->targetType, .pos = e->pos },
                    castTmp, e->targetType);
            },
            [&](ast::IntrinsicCallExpr* e) -> Value {
                // Dispatched to Phase 3 intrinsic helper
                return lowerIntrinsicCallExpr(e);
            },
            [&](auto) -> Value { return std::monostate {}; } },
        expr);
}

// =============================================================================
// Phase 3: Dedicated Intrinsic Dispatcher
// =============================================================================

Value Builder::lowerIntrinsicCallExpr(ast::IntrinsicCallExpr* e)
{
    std::string_view intrinsicName = sym_.resolve(e->intrinsicSym);

    // 1. Coroutine Suspension: "maml_yield_now"
    //    Requires passing currentFuture_ and mutating the CFG via emitCoroSuspend.
    if (intrinsicName == "maml_yield_now") {
        emitRuntimeCall(sym_.intern("maml_yield_now"), reg_.getPrimitive(types::TypeKind::Unit),
            { currentFuture_ }, e->pos);
        current_ = emitCoroSuspend(e->pos);
        return std::monostate {};
    }

    // 2. Container Length: "maml_len"
    //    Requires taking the L-value address of the container operand.
    if (intrinsicName == "maml_len") {
        if (e->arguments.empty()) {
            return std::monostate {};
        }

        Value ptrReg = addressOf(e->arguments[0]);
        const types::Type* argType = safeGetExprType(e->arguments[0]);
        const types::Type* u32Type = reg_.getPrimitive(types::TypeKind::U32);

        if (argType) {
            if (argType->kind == types::TypeKind::Vector
                || argType->kind == types::TypeKind::View) {
                return emitRuntimeCall(sym_.intern("maml_vec_len"), u32Type, { ptrReg }, e->pos);
            }
            if (argType->kind == types::TypeKind::Map) {
                return emitRuntimeCall(sym_.intern("maml_map_len"), u32Type, { ptrReg }, e->pos);
            }
            if (argType->kind == types::TypeKind::String) {
                return emitRuntimeCall(sym_.intern("maml_str_len"), u32Type, { ptrReg }, e->pos);
            }
        }

        return IntConstant { .value = 0, .type = u32Type, .pos = e->pos };
    }

    // 3. Map Key Deletion: "maml_delete"
    //    Requires taking the map L-value address and lowering key hashing/pointer args.
    if (intrinsicName == "maml_delete") {
        if (e->arguments.size() < 2) {
            return std::monostate {};
        }

        Value mapPtrReg = addressOf(e->arguments[0]);
        auto [hashVal, ptrVal, lenVal] = lowerMapKey(e->arguments[1], e->pos);

        return emitRuntimeCall(sym_.intern("maml_map_delete"),
            reg_.getPrimitive(types::TypeKind::Unit), { mapPtrReg, hashVal, ptrVal, lenVal },
            e->pos);
    }

    // 4. Default Fallback: General Runtime / Intrinsic Calls
    //    Evaluates all arguments by value linearly and dispatches to the C ABI.
    std::vector<Value> flatArgs;
    flatArgs.reserve(e->arguments.size());

    for (const auto& argExpr : e->arguments) {
        flatArgs.push_back(lowerExpr(argExpr));
    }

    return emitRuntimeCall(e->intrinsicSym, e->exprType, flatArgs, e->pos);
}

} // namespace maml::mir
