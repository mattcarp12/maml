#include "ast.h"
#include "builder.h"
#include "capability.h"
#include "cfg.h"
#include "intrinsics.h"
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
                    t = typeOf(s->value);
                }

                // We allocate on the stack if the variable is mutable OR if its ABI
                // layout is an aggregate value struct (excluding scalar/pointer handles).
                bool needsAlloca = s->isMutable || (t && t->isAggregate());

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
                locals_[aliasName] = s->cap == Capability::Mut
                    ? reg_.getPrimitive(types::TypeKind::Ptr)
                    : typeOf(s->value);

                if (auto* srcReg = std::get_if<Register>(&flatSrc)) {
                    emitCapTransfer(aliasName, srcReg->name, s->cap, s->pos);
                }
            },
            [&](ast::AssignStmt* s) {
                if (auto** faPtr = std::get_if<ast::FieldAccess*>(&s->lValue)) {
                    ast::FieldAccess* fa = *faPtr;
                    Value ptrVal = addressOf(s->lValue);
                    const types::Type* faType = typeOf(fa);
                    Value writeVal;
                    if (s->op != TokenType::ASSIGN) {
                        writeVal = emitCompoundMath(ptrVal, s->op, s->rValue, faType, s->pos);
                    } else {
                        writeVal = lowerExpr(s->rValue);
                    }
                    push(StoreInst {
                        .dstPtr = ptrVal, .value = writeVal, .type = faType, .pos = s->pos });
                    return;
                }

                if (auto** idxPtr = std::get_if<ast::IndexExpr*>(&s->lValue)) {
                    ast::IndexExpr* idx = *idxPtr;
                    const types::Type* idxType = typeOf(idx);
                    Value ptrVal = addressOf(s->lValue);
                    Value writeVal;
                    if (s->op != TokenType::ASSIGN) {
                        writeVal = emitCompoundMath(ptrVal, s->op, s->rValue, idxType, s->pos);
                    } else {
                        writeVal = lowerExpr(s->rValue);
                    }
                    push(StoreInst {
                        .dstPtr = ptrVal, .value = writeVal, .type = idxType, .pos = s->pos });
                    return;
                }

                if (auto** idPtr = std::get_if<ast::Identifier*>(&s->lValue)) {
                    ast::Identifier* ident = *idPtr;
                    SymID dstName = resolveLocal(ident->name);
                    const types::Type* idType = typeOf(ident);

                    // IMPLICIT STORE: If the local is a pointer, mutate the underlying memory
                    if (locals_[dstName]->kind == types::TypeKind::Ptr) {
                        Value ptrReg = Register { .name = dstName,
                            .type = reg_.getPrimitive(types::TypeKind::Ptr),
                            .pos = s->pos };
                        Value writeVal;
                        if (s->op != TokenType::ASSIGN) {
                            writeVal = emitCompoundMath(ptrReg, s->op, s->rValue, idType, s->pos);
                        } else {
                            writeVal = lowerExpr(s->rValue);
                        }
                        push(StoreInst {
                            .dstPtr = ptrReg, .value = writeVal, .type = idType, .pos = s->pos });
                        return;
                    }

                    // Standard assignment fallback for value types
                    locals_[dstName] = idType;
                    Value writeVal;
                    if (s->op != TokenType::ASSIGN) {
                        Value flatLHS = lowerExpr(s->lValue);
                        Value flatRHS = lowerExpr(s->rValue);
                        SymID opTmp = newTemp();
                        writeVal = emit(BinaryOpInst { .dst = opTmp,
                                            .left = flatLHS,
                                            .op = s->op,
                                            .right = flatRHS,
                                            .type = idType,
                                            .pos = s->pos },
                            opTmp, idType);
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
                const types::Type* vecType = typeOf(s->lValue);
                const types::Type* elemType = nullptr;
                if (vecType && vecType->kind == types::TypeKind::Vector) {
                    elemType = std::get<types::VectorPayload>(vecType->payload).base;
                }

                Value flatElem = lowerExpr(s->rValue);

                if (!elemType) {
                    elemType = getTypeOf(flatElem);
                }
                if (!elemType) {
                    elemType = typeOf(s->rValue);
                }

                Value boxedElem;
                if (elemType && elemType->isAggregate()) {
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
                return emitBorrow(regName, true, e->pos);
            },
            [&](ast::FieldAccess* e) -> Value {
                Value basePtr = addressOf(e->object);
                const types::Type* objType = typeOf(e->object);
                const types::Type* fieldType = typeOf(e);

                // Phase 4: Sum-kind handling for pattern-bound match arm fields
                if (objType && objType->kind == types::TypeKind::Sum) {
                    if (e->field->name == sym_.intern("discriminant")) {
                        const types::Type* i32Type = reg_.getPrimitive(types::TypeKind::I32);
                        return emitFieldAddr(basePtr, objType, e->field->name, 0, i32Type, e->pos);
                    }
                    const auto& sumPayload = std::get<types::SumPayload>(objType->payload);
                    Value rawPayloadAddr = emitFieldAddr(basePtr, objType, sym_.intern("payload"),
                        1, reg_.getPrimitive(types::TypeKind::Unknown), e->pos);

                    SymID castTmp = newPtrTemp();
                    push(BitcastPtrInst { .dst = castTmp,
                        .src = rawPayloadAddr,
                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                        .pos = e->pos });
                    Value typedPayloadPtr = Register { .name = castTmp,
                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                        .pos = e->pos };

                    for (const auto& variant : sumPayload.variants) {
                        std::vector<types::StructField> payloadFields;
                        if (!variant.fields.empty()) {
                            payloadFields = variant.fields;
                        } else {
                            for (size_t i = 0; i < variant.tupleTypes.size(); ++i) {
                                payloadFields.push_back(
                                    { sym_.intern("payload_" + std::to_string(i)),
                                        variant.tupleTypes[i] });
                            }
                        }

                        for (size_t i = 0; i < payloadFields.size(); ++i) {
                            if (payloadFields[i].name == e->field->name) {
                                const types::Type* payloadStructType
                                    = reg_.getStruct(variant.name, payloadFields);
                                return emitFieldAddr(typedPayloadPtr, payloadStructType,
                                    e->field->name, static_cast<int>(i), fieldType, e->pos);
                            }
                        }
                    }
                }

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
                    basePtr, objType, e->field->name, fieldIndex, fieldType, e->pos);
            },
            [&](ast::IndexExpr* e) -> Value {
                Value basePtr = addressOf(e->left);
                const types::Type* sourceType = typeOf(e->left);
                const types::Type* elemType = typeOf(e);

                if (sourceType) {
                    if (sourceType->kind == types::TypeKind::String
                        || sourceType->kind == types::TypeKind::View) {
                        Value rawDataPtr = loadField(basePtr, sourceType, sym_.intern("ptr"), 0,
                            reg_.getPrimitive(types::TypeKind::Ptr), e->pos);

                        const types::Type* viewElemType = reg_.getPrimitive(types::TypeKind::U8);
                        if (sourceType->kind == types::TypeKind::View) {
                            viewElemType = std::get<types::ViewPayload>(sourceType->payload).base;
                        }

                        Value idxVal = lowerExpr(e->index);
                        SymID elemPtrTmp = newPtrTemp();

                        return emit(IndexAddrInst { elemPtrTmp, rawDataPtr,
                                        reg_.getPrimitive(types::TypeKind::Ptr), idxVal,
                                        viewElemType, e->pos },
                            elemPtrTmp, reg_.getPrimitive(types::TypeKind::Ptr));
                    } else if (sourceType->kind == types::TypeKind::Vector) {
                        Value flatIdx = lowerExpr(e->index);
                        return EmitMamlVecGet(basePtr, flatIdx, e->pos);
                    }
                }

                Value idxVal = lowerExpr(e->index);
                SymID ptrTmp = newPtrTemp();
                return emit(IndexAddrInst { ptrTmp, basePtr, sourceType, idxVal, elemType, e->pos },
                    ptrTmp, reg_.getPrimitive(types::TypeKind::Ptr));
            },
            [&](auto) -> Value { return std::monostate {}; } },
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
    const types::Type* keyType = typeOf(keyExpr);

    if (keyType
        && (keyType->kind == types::TypeKind::I64 || keyType->kind == types::TypeKind::U64)) {
        SymID hashTmp = newTemp();
        Value hashVal
            = emit(CastInst { hashTmp, flatKey, reg_.getPrimitive(types::TypeKind::I64), pos },
                hashTmp, reg_.getPrimitive(types::TypeKind::I64));
        Value ptrVal = IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos };
        Value lenVal = IntConstant { 0, reg_.getPrimitive(types::TypeKind::I64), pos };
        return { hashVal, ptrVal, lenVal };
    } else if (keyType && keyType->kind == types::TypeKind::String) {
        SymID keyTmp = emitTemp(keyType);
        push(AllocaInst { keyTmp, keyType, pos });
        emitTransfer(keyTmp, flatKey, pos);
        Value safeKey = Register { keyTmp, keyType, pos };

        Value ptrVal = loadField(
            safeKey, keyType, sym_.intern("ptr"), 0, reg_.getPrimitive(types::TypeKind::Ptr), pos);
        // Load len as I64 to match %maml.String = type { ptr, i64, i1 }
        Value lenVal = loadField(
            safeKey, keyType, sym_.intern("len"), 1, reg_.getPrimitive(types::TypeKind::I64), pos);
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
                if (!s || s->statements.empty()) {
                    return std::monostate {};
                }

                enterScope();

                // 1. Lower all statements EXCEPT the trailing statement
                for (size_t i = 0; i + 1 < s->statements.size(); ++i) {
                    if (!current_)
                        break;
                    lowerStmt(s->statements[i]);
                }

                // 2. Evaluate the trailing statement exactly once as the block's yield value
                Value result = std::monostate {};
                if (current_) {
                    ast::Stmt tail = s->statements.back();
                    if (auto** yieldStmt = std::get_if<ast::YieldStmt*>(&tail)) {
                        result = lowerExpr((*yieldStmt)->value);
                    } else if (auto** exprStmt = std::get_if<ast::ExprStmt*>(&tail)) {
                        result = lowerExpr((*exprStmt)->value);
                    } else {
                        lowerStmt(tail);
                    }
                }

                exitScope();
                return result;
            },
            [&](ast::Identifier* e) -> Value {
                // 1. Check if it's a local variable in the environment
                SymID regName = resolveLocal(e->name);
                if (locals_.find(regName) != locals_.end()) {
                    const types::Type* t = locals_[regName];
                    const types::Type* exprTy = typeOf(e);

                    if (t && exprTy && t->kind == types::TypeKind::Ptr
                        && exprTy->kind != types::TypeKind::Ptr) {
                        Value ptrReg = Register { .name = regName, .type = t, .pos = e->pos };
                        return emitLoad(ptrReg, exprTy, e->pos);
                    }
                    return Register { .name = regName, .type = t, .pos = e->pos };
                }

                // 2. Phase 4: Bare variant constructor (zero-arg variant, e.g. None, Red)
                const types::Type* exprTy = typeOf(e);
                if (exprTy && exprTy->kind == types::TypeKind::Sum) {
                    const auto& sumPayload = std::get<types::SumPayload>(exprTy->payload);
                    for (const auto& variant : sumPayload.variants) {
                        if (variant.name == e->name) {
                            SymID tmp = emitTemp(exprTy);
                            push(AllocaInst { .dst = tmp, .type = exprTy, .pos = e->pos });
                            Value basePtr = emitBorrow(tmp, true, e->pos);

                            const types::Type* i32Type = reg_.getPrimitive(types::TypeKind::I32);
                            Value discVal = IntConstant {
                                .value = variant.discriminant, .type = i32Type, .pos = e->pos
                            };
                            storeField(basePtr, exprTy, discVal, sym_.intern("discriminant"), 0,
                                i32Type, e->pos);

                            return Register { .name = tmp, .type = exprTy, .pos = e->pos };
                        }
                    }
                }

                return Register { .name = regName,
                    .type = reg_.getPrimitive(types::TypeKind::Unknown),
                    .pos = e->pos };
            },
            [&](ast::IntLiteral* e) -> Value {
                const types::Type* ty = typeOf(e);
                if (!ty)
                    ty = reg_.getPrimitive(types::TypeKind::I64);
                return IntConstant { .value = e->value, .type = ty, .pos = e->pos };
            },
            [&](ast::BoolLiteral* e) -> Value {
                const types::Type* ty = typeOf(e);
                if (!ty)
                    ty = reg_.getPrimitive(types::TypeKind::Bool);
                return BoolConstant { .value = e->value, .type = ty, .pos = e->pos };
            },
            [&](ast::StringLiteral* e) -> Value {
                const types::Type* strType = typeOf(e);
                SymID tmp = emitTemp(strType);
                push(AllocaInst { .dst = tmp, .type = strType, .pos = e->pos });
                Value obj = Register { .name = tmp, .type = strType, .pos = e->pos };
                Value rawStrPtr = StringConstant { .value = std::string(e->value),
                    .type = reg_.getPrimitive(types::TypeKind::Ptr),
                    .pos = e->pos };

                storeField(obj, strType, rawStrPtr, sym_.intern("ptr"), 0,
                    reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                storeField(obj, strType,
                    IntConstant { .value = static_cast<int64_t>(e->value.length()),
                        .type = reg_.getPrimitive(types::TypeKind::I64),
                        .pos = e->pos },
                    sym_.intern("len"), 1, reg_.getPrimitive(types::TypeKind::I64), e->pos);
                storeField(obj, strType,
                    BoolConstant { false, reg_.getPrimitive(types::TypeKind::Bool), e->pos },
                    sym_.intern("is_owned"), 2, reg_.getPrimitive(types::TypeKind::Bool), e->pos);
                return obj;
            },
            [&](ast::InfixExpr* e) -> Value {
                Value flatLeft = lowerExpr(e->left);
                Value flatRight = lowerExpr(e->right);
                const types::Type* exprTy = typeOf(e);

                const types::Type* lType = getTypeOf(flatLeft);
                if (lType && lType->kind == types::TypeKind::String
                    && (e->op == TokenType::EQ || e->op == TokenType::NOT_EQ)) {
                    return flattenStringEq(e, flatLeft, flatRight);
                }

                // Coerce operands to the promoted expression type via explicit MIR CastInst
                auto coerceOperand = [&](Value val, const types::Type* targetTy) -> Value {
                    const types::Type* valTy = getTypeOf(val);
                    if (valTy && targetTy && valTy != targetTy && valTy->isInteger()
                        && targetTy->isInteger()) {
                        SymID castTmp = newTemp();
                        return emit(CastInst { castTmp, val, targetTy, e->pos }, castTmp, targetTy);
                    }
                    return val;
                };

                flatLeft = coerceOperand(flatLeft, exprTy);
                flatRight = coerceOperand(flatRight, exprTy);

                SymID tmp = newTemp();
                return emit(
                    BinaryOpInst { tmp, flatLeft, e->op, flatRight, exprTy, e->pos }, tmp, exprTy);
            },
            [&](ast::PrefixExpr* e) -> Value {
                Value flatRight = lowerExpr(e->right);
                const types::Type* exprTy = typeOf(e);
                SymID tmp = newTemp();
                return emit(UnaryOpInst { tmp, e->op, flatRight, exprTy, e->pos }, tmp, exprTy);
            },
            [&](ast::IfExpr* e) -> Value {
                Value flatCond = lowerExpr(e->condition);
                const types::Type* exprTy = typeOf(e);

                BasicBlock* thenBlock = newBlock();
                BasicBlock* mergeBlock = newBlock();
                BasicBlock* elseBlock = (e->alternative) ? newBlock() : mergeBlock;

                bool isUnit = (!exprTy || exprTy->kind == types::TypeKind::Unit);
                SymID resultTemp = NoSymbol;
                Value resultReg = std::monostate {};

                if (!isUnit) {
                    resultTemp = emitTemp(exprTy);
                    push(AllocaInst { resultTemp, exprTy, e->pos });
                    resultReg = Register { resultTemp, exprTy, e->pos };
                }

                // Helper to extract yield expressions from blocks ending in YieldStmt or ExprStmt
                auto extractBlockYield = [](ast::BlockStmt* block) -> ast::Expr {
                    if (!block || block->statements.empty())
                        return std::monostate {};
                    return std::visit(
                        [](auto&& s) -> ast::Expr {
                            using T = std::decay_t<decltype(s)>;
                            if constexpr (std::is_same_v<T, ast::YieldStmt*>
                                || std::is_same_v<T, ast::ExprStmt*>) {
                                return s->value;
                            }
                            return std::monostate {};
                        },
                        block->statements.back());
                };

                current_->terminator
                    = BranchTerminator { flatCond, thenBlock->id, elseBlock->id, e->pos };

                // Then Branch
                current_ = thenBlock;
                if (e->consequence)
                    lowerBlockStmt(e->consequence);
                bool mergeReachable = false;
                if (current_) {
                    if (!isUnit) {
                        Value thenYield = lowerExpr(extractBlockYield(e->consequence));
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
                            Value elseYield = lowerExpr(extractBlockYield(e->alternative));
                            if (!std::holds_alternative<std::monostate>(elseYield))
                                push(AssignInst { resultTemp, elseYield, e->pos });
                        }
                        if (std::holds_alternative<std::monostate>(current_->terminator))
                            current_->terminator = JumpTerminator { mergeBlock->id, e->pos };
                        mergeReachable = true;
                    }
                } else {
                    mergeReachable = true;
                }

                if (mergeReachable)
                    current_ = mergeBlock;
                else {
                    current_ = nullptr;
                    graph_->blocks.erase(mergeBlock->id);
                }

                return resultReg;
            },
            [&](ast::CallExpr* e) -> Value {
                // Phase 4: Intrinsic function checks on function identifier name
                if (auto** idPtr = std::get_if<ast::Identifier*>(&e->function)) {
                    ast::Identifier* fnId = *idPtr;
                    std::string_view fnName = sym_.resolve(fnId->name);

                    if (fnName == intrinsics::MAP_GET && e->arguments.size() == 2) {
                        // Arg 0: Address of the map container
                        Value mapPtr = addressOf(e->arguments[0].argument);
                        // Arg 1: Key (hash, pointer, and length)
                        auto [hashVal, keyPtrVal, lenVal]
                            = lowerMapKey(e->arguments[1].argument, e->pos);

                        // Emits a single linear call to maml_map_get -> returns raw Ptr (or null)
                        return EmitMamlMapGet(mapPtr, hashVal, keyPtrVal, lenVal, e->pos);
                    }

                    if (fnName == intrinsics::MAP_PUT && e->arguments.size() == 3) {
                        // Arg 0: Address of the map container
                        Value mapPtr = addressOf(e->arguments[0].argument);
                        // Arg 1: Key (hash, pointer, and length)
                        auto [hashVal, keyPtrVal, lenVal]
                            = lowerMapKey(e->arguments[1].argument, e->pos);
                        // Arg 2: Value to insert
                        Value valExpr = lowerExpr(e->arguments[2].argument);
                        const types::Type* valType = typeOf(e->arguments[2].argument);

                        Value boxedVal;
                        if (valType && valType->isAggregate()) {
                            if (auto* reg = std::get_if<Register>(&valExpr)) {
                                boxedVal = emitBorrow(reg->name, true, e->pos);
                            } else {
                                boxedVal = boxScalar(valExpr, valType, e->pos);
                            }
                        } else {
                            boxedVal = boxScalar(valExpr, valType, e->pos);
                        }

                        // Emits a single linear call to maml_map_put -> returns Unit
                        return EmitMamlMapPut(mapPtr, hashVal, keyPtrVal, lenVal, boxedVal, e->pos);
                    }

                    if (fnName == "yield_now") {
                        emitRuntimeCall(sym_.intern("maml_yield_now"),
                            reg_.getPrimitive(types::TypeKind::Unit), { currentFuture_ }, e->pos);
                        current_ = emitCoroSuspend(e->pos);
                        return std::monostate {};
                    }

                    if (fnName == "len" && !e->arguments.empty()) {
                        Value ptrReg = addressOf(e->arguments[0].argument);
                        const types::Type* argType = typeOf(e->arguments[0].argument);
                        const types::Type* u32Type = reg_.getPrimitive(types::TypeKind::U32);

                        if (argType) {
                            if (argType->kind == types::TypeKind::Vector
                                || argType->kind == types::TypeKind::View) {
                                return emitRuntimeCall(
                                    sym_.intern("maml_vec_len"), u32Type, { ptrReg }, e->pos);
                            }
                            if (argType->kind == types::TypeKind::Map) {
                                return emitRuntimeCall(
                                    sym_.intern("maml_map_len"), u32Type, { ptrReg }, e->pos);
                            }
                            if (argType->kind == types::TypeKind::String) {
                                return emitRuntimeCall(
                                    sym_.intern("maml_str_len"), u32Type, { ptrReg }, e->pos);
                            }
                        }
                        return IntConstant { .value = 0, .type = u32Type, .pos = e->pos };
                    }

                    if (fnName == "delete" && e->arguments.size() >= 2) {
                        Value mapPtrReg = addressOf(e->arguments[0].argument);
                        auto [hashVal, ptrVal, lenVal]
                            = lowerMapKey(e->arguments[1].argument, e->pos);
                        return emitRuntimeCall(sym_.intern("maml_map_delete"),
                            reg_.getPrimitive(types::TypeKind::Unit),
                            { mapPtrReg, hashVal, ptrVal, lenVal }, e->pos);
                    }

                    if (fnName == "run_executor" && !e->arguments.empty()) {
                        Value flatArg = lowerExpr(e->arguments[0].argument);
                        emitRuntimeCall(sym_.intern("maml_run_executor"),
                            reg_.getPrimitive(types::TypeKind::Ptr), { flatArg }, e->pos);
                        return emitGetFutureResult(flatArg, typeOf(e), e->pos);
                    }

                    // Phase 4: Sum Type Variant Construction Check
                    const types::Type* callType = typeOf(e);
                    if (callType && callType->kind == types::TypeKind::Sum) {
                        const auto& sumPayload = std::get<types::SumPayload>(callType->payload);
                        for (const auto& variant : sumPayload.variants) {
                            if (variant.name == fnId->name) {
                                SymID tmp = emitTemp(callType);
                                push(AllocaInst { .dst = tmp, .type = callType, .pos = e->pos });
                                Value basePtr = emitBorrow(tmp, true, e->pos);

                                const types::Type* i32Type
                                    = reg_.getPrimitive(types::TypeKind::I32);
                                Value discVal = IntConstant {
                                    .value = variant.discriminant, .type = i32Type, .pos = e->pos
                                };
                                storeField(basePtr, callType, discVal, sym_.intern("discriminant"),
                                    0, i32Type, e->pos);

                                if (!e->arguments.empty()) {
                                    Value rawPayloadAddr
                                        = emitFieldAddr(basePtr, callType, sym_.intern("payload"),
                                            1, reg_.getPrimitive(types::TypeKind::Unknown), e->pos);

                                    SymID castTmp = newPtrTemp();
                                    push(BitcastPtrInst { .dst = castTmp,
                                        .src = rawPayloadAddr,
                                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                                        .pos = e->pos });
                                    Value typedPayloadPtr = Register { .name = castTmp,
                                        .type = reg_.getPrimitive(types::TypeKind::Ptr),
                                        .pos = e->pos };

                                    std::vector<types::StructField> payloadFields;
                                    if (!variant.fields.empty()) {
                                        payloadFields = variant.fields;
                                    } else {
                                        for (size_t i = 0; i < variant.tupleTypes.size(); ++i) {
                                            payloadFields.push_back(
                                                { sym_.intern("payload_" + std::to_string(i)),
                                                    variant.tupleTypes[i] });
                                        }
                                    }

                                    const types::Type* payloadStructType
                                        = reg_.getStruct(variant.name, payloadFields);

                                    for (size_t i = 0; i < e->arguments.size(); ++i) {
                                        Value argValue = lowerExpr(e->arguments[i].argument);
                                        const types::Type* fieldType = (i < payloadFields.size())
                                            ? payloadFields[i].type
                                            : getTypeOf(argValue);
                                        SymID fieldName = (i < payloadFields.size())
                                            ? payloadFields[i].name
                                            : sym_.intern("payload_" + std::to_string(i));

                                        storeField(typedPayloadPtr, payloadStructType, argValue,
                                            fieldName, static_cast<int>(i), fieldType, e->pos);
                                    }
                                }
                                return Register { .name = tmp, .type = callType, .pos = e->pos };
                            }
                        }
                    }
                }

                // General Function Call Fallback
                Value flatFunc = lowerExpr(e->function);
                std::vector<Value> flatArgs;
                std::vector<bool> argConsumed;

                for (const auto& arg : e->arguments) {
                    const types::Type* argType = typeOf(arg.argument);
                    const types::Type* resultType = lowerParamType(argType, arg.cap);

                    SymID argTmp = emitTemp(resultType);
                    if (arg.cap == Capability::Mut) {
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
                    argConsumed.push_back(arg.cap == Capability::Own);
                }

                const types::Type* callType = typeOf(e);
                SymID tmp = emitTemp(callType);
                return emit(CallInst { tmp, flatFunc, flatArgs, argConsumed, callType, e->pos },
                    tmp, callType);
            },
            [&](ast::FieldAccess* e) -> Value {
                Value ptrVal = addressOf(e);
                return emitLoad(ptrVal, typeOf(e), e->pos);
            },
            [&](ast::IndexExpr* e) -> Value {
                Value ptrVal = addressOf(e);
                const types::Type* exprTy = typeOf(e);
                Value rawVal = emitLoad(ptrVal, exprTy, e->pos);
                return rawVal;
            },
            [&](ast::SliceExpr* e) -> Value {
                Value basePtr = addressOf(e->left);
                const types::Type* sourceType = typeOf(e->left);
                const types::Type* sliceType = typeOf(e);

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

                SymID resultTmp = emitTemp(sliceType);
                push(AllocaInst { resultTmp, sliceType, e->pos });
                Value result = Register { resultTmp, sliceType, e->pos };

                storeField(result, sliceType, newDataPtr, sym_.intern("ptr"), 0,
                    reg_.getPrimitive(types::TypeKind::Ptr), e->pos);
                storeField(result, sliceType, newLen, sym_.intern("len"), 1,
                    reg_.getPrimitive(types::TypeKind::I64), e->pos);

                if (sliceType->kind == types::TypeKind::String) {
                    storeField(result, sliceType,
                        BoolConstant { false, reg_.getPrimitive(types::TypeKind::Bool), e->pos },
                        sym_.intern("is_owned"), 2, reg_.getPrimitive(types::TypeKind::Bool),
                        e->pos);
                }
                return result;
            },
            [&](ast::AwaitExpr* e) -> Value {
                Value flatTask = lowerExpr(e->value);
                const types::Type* exprTy = typeOf(e);
                emitRuntimeCall(sym_.intern("maml_task_await"),
                    reg_.getPrimitive(types::TypeKind::Unit), { flatTask, currentFuture_ }, e->pos);
                current_ = emitCoroSuspend(e->pos);
                return emitGetFutureResult(flatTask, exprTy, e->pos);
            },
            [&](ast::SpawnExpr* e) -> Value {
                Value flatFuture = lowerExpr(e->value);
                emitRuntimeCall(sym_.intern("maml_spawn_task"),
                    reg_.getPrimitive(types::TypeKind::Unit), { flatFuture }, e->pos);
                return flatFuture;
            },
            [&](ast::CompositeLiteral* e) -> Value {
                const types::Type* t = typeOf(e);
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
                if (t->kind == types::TypeKind::Sum) {
                    return lowerSumTypeLiteral(e, t);
                }

                return std::monostate {};
            },
            [&](auto) -> Value { return std::monostate {}; } },
        expr);
}

} // namespace maml::mir