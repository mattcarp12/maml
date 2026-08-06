#include "types.h"
#include "sym.h"
#include <cstddef>
#include <cstdint>
#include <format>
#include <string>

namespace maml::types {

bool Type::isInteger() const { return kind >= TypeKind::I8 && kind <= TypeKind::U128; }

bool Type::canRepresentInt(int64_t value) const
{
    switch (kind) {
    case TypeKind::I8:
        return value >= -128 && value <= 127;
    case TypeKind::I16:
        return value >= -32768 && value <= 32767;
    case TypeKind::I32:
        return value >= -2147483648LL && value <= 2147483647LL;
    case TypeKind::I64:
        return true;
    case TypeKind::I128:
        return true; // C++ limits us to checking 64-bit bounds natively anyway
    case TypeKind::U8:
        return value >= 0 && value <= 255;
    case TypeKind::U16:
        return value >= 0 && value <= 65535;
    case TypeKind::U32:
        return value >= 0 && value <= 4294967295LL;
    case TypeKind::U64:
        return value >= 0;
    case TypeKind::U128:
        return value >= 0;
    default:
        return false;
    }
}

bool Type::isCopyable() const
{
    if (kind >= TypeKind::I8 && kind <= TypeKind::Unit)
        return true;

    if (kind == TypeKind::Struct) {
        const auto& p = std::get<StructPayload>(payload);
        for (const auto& f : p.fields) {
            if (!f.type->isCopyable())
                return false;
        }
        return true;
    }

    if (kind == TypeKind::Array) {
        return std::get<ArrayPayload>(payload).base->isCopyable();
    }

    // Vec, Map, String, Views, Buffers, Futures are all heap/reference structures
    return false;
}

std::string Type::toString(const SymbolTable& sym) const
{
    switch (kind) {
    case TypeKind::I8:
        return "i8";
    case TypeKind::I16:
        return "i16";
    case TypeKind::I32:
        return "i32";
    case TypeKind::I64:
        return "i64";
    case TypeKind::I128:
        return "i128";
    case TypeKind::U8:
        return "u8";
    case TypeKind::U16:
        return "u16";
    case TypeKind::U32:
        return "u32";
    case TypeKind::U64:
        return "u64";
    case TypeKind::U128:
        return "u128";
    case TypeKind::F32:
        return "f32";
    case TypeKind::F64:
        return "f64";
    case TypeKind::Bool:
        return "bool";
    case TypeKind::Char:
        return "char";
    case TypeKind::Unit:
        return "unit";
    case TypeKind::String:
        return "string";
    case TypeKind::Ptr:
        return "ptr";
    case TypeKind::Any:
        return "any";
    case TypeKind::Unknown:
        return "unknown";

    case TypeKind::Buffer:
        return std::format("Buffer<{}>", std::get<BufferPayload>(payload).base->toString(sym));
    case TypeKind::Array: {
        const auto& p = std::get<ArrayPayload>(payload);
        return std::format("[{}]{}", p.size, p.base->toString(sym));
    }
    case TypeKind::View: {
        const auto& p = std::get<ViewPayload>(payload);
        return std::format("[]{}{}", p.base->toString(sym), p.isMut ? "mut" : "");
    }
    case TypeKind::Vector:
        return std::format("Vec<{}>", std::get<VectorPayload>(payload).base->toString(sym));
    case TypeKind::Map: {
        const auto& p = std::get<MapPayload>(payload);
        return std::format("Map<{}, {}>", p.key->toString(sym), p.value->toString(sym));
    }
    case TypeKind::Future:
        return std::format("Future<{}>", std::get<FuturePayload>(payload).base->toString(sym));
    case TypeKind::Struct:
        return std::string(sym.resolve(std::get<StructPayload>(payload).name));
    case TypeKind::Sum: {
        const auto& p = std::get<SumPayload>(payload);
        std::string res = std::string(sym.resolve(p.baseName));
        if (!p.typeArgs.empty()) {
            res += "<";
            for (size_t i = 0; i < p.typeArgs.size(); ++i) {
                res += p.typeArgs[i]->toString(sym);
                if (i != p.typeArgs.size() - 1)
                    res += ", ";
            }
            res += ">";
        }
        return res;
    }
    case TypeKind::Function: {
        const auto& p = std::get<FunctionPayload>(payload);
        std::string res = "fn(";
        for (size_t i = 0; i < p.params.size(); ++i) {
            res += p.params[i]->toString(sym);
            if (i != p.params.size() - 1)
                res += ", ";
        }
        res += ") " + p.returnType->toString(sym);
        return res;
    }
    }
    return "";
}


} // namespace maml::types