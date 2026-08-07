#pragma once

#include "sym.h"
#include "token.h"
#include "types.h"
#include <cstdint>
#include <string>
#include <type_traits>
#include <variant>
#include <vector>

namespace maml::mir {

using BlockID = int32_t;
constexpr BlockID InvalidBlock = -1;

// ============================================================================
// Values
// ============================================================================

struct Register {
    SymID name;
    const types::Type* type = nullptr;
    Position pos;
};

struct IntConstant {
    int64_t value;
    const types::Type* type = nullptr;
    Position pos;
};

struct BoolConstant {
    bool value;
    const types::Type* type = nullptr;
    Position pos;
};

struct StringConstant {
    std::string value;
    const types::Type* type = nullptr;
    Position pos;
};

using Value = std::variant<std::monostate, Register, IntConstant, BoolConstant, StringConstant>;

// Helpers to safely extract data from a Value variant
const types::Type* getTypeOf(const Value& val);
Position getPosOf(const Value& val);

// ============================================================================
// Instructions
// ============================================================================

struct AddressOfInst {
    SymID dst;
    SymID src;
    Position pos;
};

struct AllocaInst {
    SymID dst;
    const types::Type* type = nullptr; // The type being allocated on the stack
    Position pos;
};

struct AssignInst {
    SymID dst;
    Value rValue;
    Position pos;
};

struct BinaryOpInst {
    SymID dst;
    Value left;
    TokenType op; // Replacing the raw string operator for faster checking
    Value right;
    const types::Type* type = nullptr;
    Position pos;
};

struct BitcastPtrInst {
    SymID dst;
    Value src;
    const types::Type* type = nullptr;
    Position pos;
};

struct BorrowInst {
    SymID dst;
    bool isMut;
    SymID src;
    Position pos;
};

struct CallInst {
    SymID dst;
    Value function;
    std::vector<Value> arguments;
    std::vector<bool> argConsumed;
    const types::Type* type = nullptr;
    Position pos;
};

struct CastInst {
    SymID dst;
    Value src;
    const types::Type* type = nullptr;
    Position pos;
};

struct CopyInst {
    SymID dst;
    SymID src;
    Position pos;
};

struct CoroPrologueInst {
    Position pos;
};

struct CoroPromisePtrInst {
    SymID dst;
    Value handle;
    const types::Type* type = nullptr;
    Position pos;
};

struct FieldAddrInst {
    SymID dst;
    Value object;
    const types::Type* objectType = nullptr;
    SymID fieldName;
    int fieldIndex;
    const types::Type* fieldType = nullptr;
    std::vector<const types::Type*> variantLayout;
    Position pos;
};

struct IndexAddrInst {
    SymID dst;
    Value source;
    const types::Type* sourceType = nullptr;
    Value index;
    const types::Type* type = nullptr;
    Position pos;
};

struct KeepAliveInst {
    SymID src;
    Position pos;
};

struct LoadPtrInst {
    SymID dst;
    Value ptr;
    const types::Type* type = nullptr;
    Position pos;
};

struct MoveInst {
    SymID dst;
    SymID src;
    Position pos;
};

struct StoreInst {
    Value dstPtr;
    Value value;
    const types::Type* type = nullptr;
    Position pos;
};

struct UnaryOpInst {
    SymID dst;
    TokenType op;
    Value operand;
    const types::Type* type = nullptr;
    Position pos;
};

using Instruction = std::variant<std::monostate, AddressOfInst, AllocaInst, AssignInst,
    BinaryOpInst, BitcastPtrInst, BorrowInst, CallInst, CastInst, CopyInst, CoroPrologueInst,
    CoroPromisePtrInst, FieldAddrInst, IndexAddrInst, KeepAliveInst, LoadPtrInst, MoveInst,
    StoreInst, UnaryOpInst>;

// ============================================================================
// Terminators
// ============================================================================

struct BranchTerminator {
    Value condition;
    BlockID trueTarget = InvalidBlock;
    BlockID falseTarget = InvalidBlock;
    Position pos;
};

struct CoroFinalSuspendTerminator {
    Value value;
    BlockID suspendBlock = InvalidBlock;
    BlockID cleanupBlock = InvalidBlock;
    Position pos;
};

struct CoroSuspendTerminator {
    BlockID resumeBlock = InvalidBlock;
    BlockID cleanupBlock = InvalidBlock;
    BlockID suspendBlock = InvalidBlock;
    Position pos;
};

struct CoroYieldTerminator {
    Position pos;
};

struct JumpTerminator {
    BlockID target = InvalidBlock;
    Position pos;
};

struct ReturnTerminator {
    Value value;
    Position pos;
};

struct UnreachableTerminator {
    Position pos;
};

using Terminator = std::variant<std::monostate, BranchTerminator, CoroFinalSuspendTerminator,
    CoroSuspendTerminator, CoroYieldTerminator, JumpTerminator, ReturnTerminator,
    UnreachableTerminator>;

inline const types::Type* getTypeOf(const Value& val)
{
    return std::visit(
        [](auto&& v) -> const types::Type* {
            using T = std::decay_t<decltype(v)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                return nullptr;
            } else {
                return v.type;
            }
        },
        val);
}

inline Position getPosOf(const Value& val)
{
    return std::visit(
        [](auto&& v) -> Position {
            using T = std::decay_t<decltype(v)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                return Position {};
            } else {
                return v.pos;
            }
        },
        val);
}

inline Position getPosOf(const Instruction& inst)
{
    return std::visit(
        [](auto&& v) -> Position {
            using T = std::decay_t<decltype(v)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                return Position {};
            } else {
                return v.pos;
            }
        },
        inst);
}

inline Position getPosOf(const Terminator& term)
{
    return std::visit(
        [](auto&& v) -> Position {
            using T = std::decay_t<decltype(v)>;
            if constexpr (std::is_same_v<T, std::monostate>) {
                return Position {};
            } else {
                return v.pos;
            }
        },
        term);
}

} // namespace maml::mir
