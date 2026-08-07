#include "ast.h"
#include "builder.h"
#include "cfg.h"
#include "compiler_context.h"
#include "mir.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"
#include "type_resolver.h"
#include "types.h"

#include <memory>
#include <string>
#include <utility>
#include <variant>
#include <vector>

namespace maml::mir {

Builder::Builder(CompilerContext& ctx)
    : ctx_(ctx)
    , reg_(ctx.types.registry)
    , sym_(ctx.symbols)
{
}

// =============================================================================
// Top-Level Coordination
// =============================================================================

std::unique_ptr<Program> Builder::buildProgram(ast::Program* astProg)
{
    if (!astProg)
        return nullptr;

    auto mirProg = std::make_unique<Program>();

    for (const auto& decl : astProg->decls) {
        if (auto td = std::get_if<ast::TypeDecl*>(&decl)) {
            mirProg->typeDecls.push_back(*td);
        } else if (auto fn = std::get_if<ast::FnDecl*>(&decl)) {
            buildFn(*fn, *mirProg);
        }
    }
    return mirProg;
}

void Builder::buildFn(ast::FnDecl* fn, Program& prog)
{
    Function mirFn;
    mirFn.name = fn->name;
    mirFn.isAsync = fn->isAsync;
    mirFn.isExtern = fn->isExtern;

    // 1. Reset function-level builder state FIRST
    nextID_ = 0;
    tempCount_ = 0;
    loops_.clear();
    env_.clear();
    nameFreq_.clear();
    locals_.clear();
    currentFuture_ = std::monostate {};

    // 2. Open function scope before processing parameters
    enterScope();

    // Resolve return type from semantic pass decoration
    mirFn.returnType = reg_.getPrimitive(types::TypeKind::Unit);
    if (!std::holds_alternative<std::monostate>(fn->returnType)) {
        mirFn.returnType = typeOf(fn->returnType);
    }

    types::TypeResolver resolver(reg_, sym_, ctx_.globalScope);

    mirFn.returnType = reg_.getPrimitive(types::TypeKind::Unit);
    if (!std::holds_alternative<std::monostate>(fn->returnType)) {
        mirFn.returnType = typeOf(fn->returnType);
        if (!mirFn.returnType) {
            mirFn.returnType = resolver.resolve(fn->returnType, ctx_.diagnostics);
        }
    }

    // 3. Populate parameters for ALL functions
    for (const auto& p : fn->params) {
        const types::Type* baseType = typeOf(p.type);
        if (!baseType) {
            baseType = resolver.resolve(p.type, ctx_.diagnostics);
        }
        const types::Type* loweredType = lowerParamType(baseType, p.cap);

        SymID paramName = fn->isExtern ? p.name : defineLocal(p.name);
        locals_[paramName] = loweredType;

        mirFn.params.push_back(Param { paramName, loweredType });
    }

    // 4. Early exit for extern functions (make sure to exit scope!)
    if (fn->isExtern) {
        exitScope();
        prog.functions.push_back(std::move(mirFn));
        return;
    }

    // 5. Set up CFG and Graph for non-extern functions
    graph_ = new Graph();
    graph_->params = mirFn.params;

    BasicBlock* entry = newBlock();
    graph_->entry = entry->id;
    current_ = entry;

    if (fn->isAsync) {
        push(CoroPrologueInst { fn->pos });
        SymID futReg = sym_.intern("__coro_handle");
        locals_[futReg] = reg_.getPrimitive(types::TypeKind::Ptr);
        currentFuture_ = Register { futReg, reg_.getPrimitive(types::TypeKind::Ptr), fn->pos };
    }

    lowerBlockStmt(fn->body);

    // --- FIX #2: Handle unterminated blocks based on return type ---
    if (current_ != nullptr && std::holds_alternative<std::monostate>(current_->terminator)) {
        if (fn->isAsync) {
            BasicBlock* suspendBlock = newBlock();
            BasicBlock* cleanupBlock = newBlock();

            current_->terminator = CoroFinalSuspendTerminator { std::monostate {}, suspendBlock->id,
                cleanupBlock->id, fn->end };
            cleanupBlock->terminator = CoroYieldTerminator { fn->end };
            suspendBlock->terminator = UnreachableTerminator { fn->end };
        } else if (mirFn.returnType && mirFn.returnType->kind != types::TypeKind::Unit) {
            // Non-void function reached end of block without a terminator.
            // Emit unreachable instead of a void ReturnTerminator to prevent LLVM 'ret void' crash.
            current_->terminator = UnreachableTerminator { fn->end };
        } else {
            current_->terminator = ReturnTerminator { std::monostate {}, fn->end };
        }
    }
    // ---------------------------------------------------------------

    exitScope();

    mirFn.graph.reset(graph_);
    mirFn.locals = std::move(locals_);
    prog.functions.push_back(std::move(mirFn));
}

// =============================================================================
// Scope and Name Generation
// =============================================================================

void Builder::enterScope() { env_.emplace_back(); }
void Builder::exitScope()
{
    if (!env_.empty())
        env_.pop_back();
}

SymID Builder::defineLocal(SymID originalName)
{
    int count = nameFreq_[originalName]++;
    SymID uniqueName = originalName;
    if (count > 0) {
        std::string gen = std::string(sym_.resolve(originalName)) + "_" + std::to_string(count);
        uniqueName = sym_.intern(gen);
    }
    env_.back()[originalName] = uniqueName;
    return uniqueName;
}

SymID Builder::resolveLocal(SymID originalName)
{
    for (auto it = env_.rbegin(); it != env_.rend(); ++it) {
        if (it->count(originalName))
            return it->at(originalName);
    }
    return originalName;
}

// =============================================================================
// Block and Register Allocation
// =============================================================================

BasicBlock* Builder::newBlock()
{
    auto block = std::make_unique<BasicBlock>();
    block->id = nextID_++;
    BasicBlock* ptr = block.get();
    graph_->blocks[ptr->id] = std::move(block);
    return ptr;
}

SymID Builder::newTemp()
{
    tempCount_++;
    return sym_.intern("_t" + std::to_string(tempCount_));
}

SymID Builder::newPtrTemp()
{
    SymID tmp = newTemp();
    locals_[tmp] = reg_.getPrimitive(types::TypeKind::Ptr);
    return tmp;
}

SymID Builder::emitTemp(const types::Type* t)
{
    SymID tmp = newTemp();
    locals_[tmp] = t;
    return tmp;
}

// =============================================================================
// Core Emission Primitives
// =============================================================================

void Builder::push(const Instruction& inst)
{
    // Hoist all stack allocations to the function's entry block
    if (std::holds_alternative<AllocaInst>(inst)) {
        if (graph_ && graph_->entry != InvalidBlock) {
            graph_->blocks[graph_->entry]->statements.push_back(inst);
            return;
        }
    }

    if (current_)
        current_->statements.push_back(inst);
}

Value Builder::emit(Instruction inst, SymID dst, const types::Type* t)
{
    locals_[dst] = t;
    push(inst);
    return Register { dst, t, mir::getPosOf(inst) };
}

Value Builder::emitLoad(Value ptr, const types::Type* t, Position pos)
{
    SymID tmp = emitTemp(t);
    push(LoadPtrInst { tmp, ptr, t, pos });
    return Register { tmp, t, pos };
}

Value Builder::emitFieldAddr(Value obj, const types::Type* objType, SymID fieldName, int fieldIdx,
    const types::Type* fieldType, Position pos)
{
    SymID addr = newPtrTemp();
    push(FieldAddrInst { addr, obj, objType, fieldName, fieldIdx, fieldType, {}, pos });
    return Register { addr, reg_.getPrimitive(types::TypeKind::Ptr), pos };
}

void Builder::storeField(Value obj, const types::Type* objType, Value val, SymID fieldName,
    int fieldIdx, const types::Type* fieldType, Position pos)
{
    Value addr = emitFieldAddr(obj, objType, fieldName, fieldIdx, fieldType, pos);
    push(StoreInst { addr, val, fieldType, pos });
}

Value Builder::loadField(Value obj, const types::Type* objType, SymID fieldName, int fieldIdx,
    const types::Type* fieldType, Position pos)
{
    Value addr = emitFieldAddr(obj, objType, fieldName, fieldIdx, fieldType, pos);
    return emitLoad(addr, fieldType, pos);
}

Value Builder::emitBorrow(SymID src, bool isMut, Position pos)
{
    SymID dst = newPtrTemp();
    push(BorrowInst { dst, isMut, src, pos });
    return Register { dst, reg_.getPrimitive(types::TypeKind::Ptr), pos };
}

// =============================================================================
// Memory Transfer Operations
// =============================================================================

void Builder::emitTransfer(SymID dst, Value val, Position pos)
{
    if (locals_[dst]->kind == types::TypeKind::Unit)
        return;

    if (auto* reg = std::get_if<Register>(&val)) {
        if (reg->type && reg->type->ownsHeapMemory()) {
            push(MoveInst { dst, reg->name, pos });
        } else {
            push(CopyInst { dst, reg->name, pos });
        }
        return;
    }
    push(AssignInst { dst, val, pos });
}

void Builder::emitCapTransfer(SymID dst, SymID src, Capability cap, Position pos)
{
    const types::Type* dstType = locals_[dst];
    bool isPtr = (dstType->kind == types::TypeKind::Ptr);

    switch (cap) {
    case Capability::Mut:
        push(BorrowInst { dst, true, src, pos });
        break;
    case Capability::Ro:
        if (isPtr)
            push(BorrowInst { dst, false, src, pos });
        else
            push(CopyInst { dst, src, pos });
        break;
    case Capability::Own:
        push(MoveInst { dst, src, pos });
        break;
    default:
        push(CopyInst { dst, src, pos });
        break;
    }
}

const types::Type* Builder::lowerParamType(const types::Type* t, Capability cap)
{
    if (cap == Capability::Mut)
        return reg_.getPrimitive(types::TypeKind::Ptr);
    if (cap == Capability::Ro && t->ownsHeapMemory())
        return reg_.getPrimitive(types::TypeKind::Ptr);
    return t;
}

Value Builder::boxScalar(Value val, const types::Type* t, Position pos)
{
    SymID slot = emitTemp(t);
    push(AllocaInst { slot, t, pos });
    Value ptrVal = emitBorrow(slot, true, pos);
    push(StoreInst { ptrVal, val, t, pos });
    return ptrVal;
}

Value Builder::emitRuntimeCall(
    SymID funcSym, const types::Type* retType, const std::vector<Value>& args, Position pos)
{
    bool isVoid = !retType || retType->kind == types::TypeKind::Unit;
    SymID dst = isVoid ? sym_.intern("_") : emitTemp(retType);

    std::vector<bool> consumed(args.size(), false);
    push(CallInst { .dst = dst,
        .function = Register { .name = funcSym,
            .type = reg_.getPrimitive(types::TypeKind::Unknown),
            .pos = pos },
        .arguments = args,
        .argConsumed = consumed,
        .type = retType,
        .pos = pos });

    if (isVoid)
        return std::monostate {};
    return Register { dst, retType, pos };
}

// =============================================================================
// Structs and Variants
// =============================================================================

BasicBlock* Builder::emitCoroSuspend(Position pos)
{
    BasicBlock* resumeBlock = newBlock();
    BasicBlock* cleanupBlock = newBlock();
    BasicBlock* suspendBlock = newBlock();

    current_->terminator
        = CoroSuspendTerminator { resumeBlock->id, cleanupBlock->id, suspendBlock->id, pos };
    cleanupBlock->terminator = JumpTerminator { suspendBlock->id, pos };
    suspendBlock->terminator = CoroYieldTerminator { pos };

    return resumeBlock;
}

} // namespace maml::mir