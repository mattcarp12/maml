#pragma once

#include "cfg.h"
#include "mir.h"
#include "sym.h"
#include <cstddef>
#include <iostream>
#include <type_traits>
#include <variant>

namespace maml::mir {

inline void dumpValue(std::ostream& os, const Value& val, const SymbolTable& sym)
{
    std::visit(
        [&](auto&& v) {
            using T = std::decay_t<decltype(v)>;
            if constexpr (std::is_same_v<T, Register>) {
                os << "%" << sym.resolve(v.name);
            } else if constexpr (std::is_same_v<T, IntConstant>) {
                os << v.value;
            } else if constexpr (std::is_same_v<T, BoolConstant>) {
                os << (v.value ? "true" : "false");
            } else if constexpr (std::is_same_v<T, StringConstant>) {
                os << "\"" << v.value << "\"";
            } else {
                os << "void";
            }
        },
        val);
}

inline void dumpInstruction(std::ostream& os, const Instruction& inst, const SymbolTable& sym)
{
    std::visit(
        [&](auto&& i) {
            using T = std::decay_t<decltype(i)>;
            if constexpr (std::is_same_v<T, std::monostate>)
                return;

            os << "    ";
            if constexpr (std::is_same_v<T, AssignInst>) {
                os << "%" << sym.resolve(i.dst) << " = assign ";
                dumpValue(os, i.rValue, sym);
            } else if constexpr (std::is_same_v<T, BinaryOpInst>) {
                os << "%" << sym.resolve(i.dst) << " = binop ";
                dumpValue(os, i.left, sym);
                os << " op " << static_cast<int>(i.op) << " ";
                dumpValue(os, i.right, sym);
            } else if constexpr (std::is_same_v<T, CallInst>) {
                os << "%" << sym.resolve(i.dst) << " = call ";
                dumpValue(os, i.function, sym);
                os << "(";
                for (size_t idx = 0; idx < i.arguments.size(); ++idx) {
                    if (idx > 0)
                        os << ", ";
                    dumpValue(os, i.arguments[idx], sym);
                }
                os << ")";
            } else if constexpr (std::is_same_v<T, AllocaInst>) {
                os << "%" << sym.resolve(i.dst) << " = alloca";
                if (i.type)
                    os << " : " << i.type->toString(sym);
                else
                    os << " : <null type!>";
            } else if constexpr (std::is_same_v<T, LoadPtrInst>) {
                os << "%" << sym.resolve(i.dst) << " = load ";
                dumpValue(os, i.ptr, sym);
            } else if constexpr (std::is_same_v<T, StoreInst>) {
                os << "store ";
                dumpValue(os, i.value, sym);
                os << " -> ";
                dumpValue(os, i.dstPtr, sym);
            } else if constexpr (std::is_same_v<T, MoveInst>) {
                os << "%" << sym.resolve(i.dst) << " = move %" << sym.resolve(i.src);
            } else if constexpr (std::is_same_v<T, CopyInst>) {
                os << "%" << sym.resolve(i.dst) << " = copy %" << sym.resolve(i.src);
            } else if constexpr (std::is_same_v<T, AddressOfInst>) {
                os << "%" << sym.resolve(i.dst) << " = addr_of %" << sym.resolve(i.src);
            } else if constexpr (std::is_same_v<T, BitcastPtrInst>) {
                os << "%" << sym.resolve(i.dst) << " = bitcast ";
                dumpValue(os, i.src, sym);
            } else if constexpr (std::is_same_v<T, BorrowInst>) {
                os << "%" << sym.resolve(i.dst) << " = borrow" << (i.isMut ? " mut " : " ") << "%"
                   << sym.resolve(i.src);
            } else if constexpr (std::is_same_v<T, CastInst>) {
                os << "%" << sym.resolve(i.dst) << " = cast ";
                dumpValue(os, i.src, sym);
            } else if constexpr (std::is_same_v<T, CoroPrologueInst>) {
                os << "coro_prologue";
            } else if constexpr (std::is_same_v<T, CoroPromisePtrInst>) {
                os << "%" << sym.resolve(i.dst) << " = coro_promise_ptr ";
                dumpValue(os, i.handle, sym);
            } else if constexpr (std::is_same_v<T, FieldAddrInst>) {
                os << "%" << sym.resolve(i.dst) << " = field_addr ";
                dumpValue(os, i.object, sym);
                os << ", " << sym.resolve(i.fieldName) << " (idx " << i.fieldIndex << ")";
            } else if constexpr (std::is_same_v<T, IndexAddrInst>) {
                os << "%" << sym.resolve(i.dst) << " = index_addr ";
                dumpValue(os, i.source, sym);
                os << "[";
                dumpValue(os, i.index, sym);
                os << "]";
            } else if constexpr (std::is_same_v<T, KeepAliveInst>) {
                os << "keep_alive %" << sym.resolve(i.src);
            } else if constexpr (std::is_same_v<T, UnaryOpInst>) {
                os << "%" << sym.resolve(i.dst) << " = unop " << static_cast<int>(i.op) << " ";
                dumpValue(os, i.operand, sym);
            } else {
                os << "/* instruction */";
            }
            os << "\n";
        },
        inst);
}

inline void dumpTerminator(std::ostream& os, const Terminator& term, const SymbolTable& sym)
{
    std::visit(
        [&](auto&& t) {
            using T = std::decay_t<decltype(t)>;
            if constexpr (std::is_same_v<T, std::monostate>)
                return;

            os << "    ";
            if constexpr (std::is_same_v<T, JumpTerminator>) {
                os << "jump bb" << t.target;
            } else if constexpr (std::is_same_v<T, BranchTerminator>) {
                os << "branch ";
                dumpValue(os, t.condition, sym);
                os << ", true: bb" << t.trueTarget << ", false: bb" << t.falseTarget;
            } else if constexpr (std::is_same_v<T, ReturnTerminator>) {
                os << "return ";
                dumpValue(os, t.value, sym);
            } else if constexpr (std::is_same_v<T, UnreachableTerminator>) {
                os << "unreachable";
            } else if constexpr (std::is_same_v<T, CoroSuspendTerminator>) {
                os << "coro_suspend resume: bb" << t.resumeBlock << ", cleanup: bb"
                   << t.cleanupBlock << ", suspend: bb" << t.suspendBlock;
            } else if constexpr (std::is_same_v<T, CoroFinalSuspendTerminator>) {
                os << "coro_final_suspend ";
                dumpValue(os, t.value, sym);
                os << ", suspend: bb" << t.suspendBlock << ", cleanup: bb" << t.cleanupBlock;
            } else if constexpr (std::is_same_v<T, CoroYieldTerminator>) {
                os << "coro_yield";
            } else {
                os << "/* terminator */";
            }
            os << "\n";
        },
        term);
}

inline void dumpProgramMIR(std::ostream& os, const Program& mirProg, const SymbolTable& sym)
{
    for (const auto& fn : mirProg.functions) {
        os << "fn " << sym.resolve(fn.name) << "(";
        for (size_t idx = 0; idx < fn.params.size(); ++idx) {
            if (idx > 0)
                os << ", ";
            const auto& param = fn.params[idx];
            os << "%" << sym.resolve(param.name);
            if (param.type)
                // os << ": " << types::toString(param.type);
                os << ": " << param.type->toString(sym);
        }
        os << ")";
        if (fn.returnType)
            os << " -> " << fn.returnType->toString(sym);
        os << " {\n";
        if (fn.graph) {
            for (const auto* block : fn.graph->sortedBlocks()) {
                if (!block)
                    continue;
                os << "  bb" << block->id << ":\n";
                for (const auto& inst : block->statements) {
                    dumpInstruction(os, inst, sym);
                }
                dumpTerminator(os, block->terminator, sym);
            }
        }
        os << "}\n\n";
    }
}

} // namespace maml::mir