#include "type_resolver.h"
#include "ast.h"
#include "diagnostics.h"
#include "scope.h"
#include "type_lowering.h"
#include "types.h"
#include <format>
#include <string_view>
#include <type_traits>
#include <utility>
#include <variant>
#include <vector>

namespace maml::types {
const Type* TypeResolver::resolve(const ast::TypeExpr& expr, Diagnostics& diags)
{
    return std::visit(
        [this, &diags](auto&& arg) -> const Type* {
            using T = std::decay_t<decltype(arg)>;
            if constexpr (std::is_same_v<T, std::monostate> || !std::is_pointer_v<T>) {
                return registry_.getPrimitive(TypeKind::Unknown);
            } else if (!arg) {
                return registry_.getPrimitive(TypeKind::Unknown);
            } else if constexpr (std::is_same_v<T, ast::ArrayTypeExpr*>) {
                const Type* base = this->resolve(arg->base, diags);
                return registry_.getArray(base, arg->size);
            } else if constexpr (std::is_same_v<T, ast::StructTypeExpr*>) {
                std::vector<StructField> fields;
                fields.reserve(arg->fields.size());
                for (const auto& f : arg->fields) {
                    fields.push_back({ f.name, this->resolve(f.type, diags) });
                }
                return registry_.getStruct(arg->name, std::move(fields));
            } else if constexpr (std::is_same_v<T, ast::SumTypeExpr*>) {
                std::vector<SumVariant> variants;
                variants.reserve(arg->variants.size());
                int disc = 0;
                for (const auto& v : arg->variants) {
                    std::vector<const Type*> tupleTys;
                    for (const auto& t : v.tupleFields) {
                        tupleTys.push_back(this->resolve(t, diags));
                    }
                    std::vector<StructField> sFields;
                    for (const auto& f : v.fields) {
                        sFields.push_back({ f.name, this->resolve(f.type, diags) });
                    }
                    variants.push_back(
                        SumVariant { v.name, disc++, std::move(tupleTys), std::move(sFields) });
                }
                return registry_.getSum(arg->name, std::move(variants));
            } else if constexpr (std::is_same_v<T, ast::GenericTypeExpr*>) {
                if (!arg->name)
                    return registry_.getPrimitive(TypeKind::Unknown);
                std::string_view name = sym_.resolve(arg->name->name);

                if (name == "Vec" && arg->args.size() == 1) {
                    return registry_.getVector(this->resolve(arg->args[0], diags));
                }
                if (name == "Buffer" && arg->args.size() == 1) {
                    return registry_.getBuffer(this->resolve(arg->args[0], diags));
                }
                if (name == "Future" && arg->args.size() == 1) {
                    return registry_.getFuture(this->resolve(arg->args[0], diags));
                }
                if (name == "Map" && arg->args.size() == 2) {
                    const Type* k = this->resolve(arg->args[0], diags);
                    const Type* v = this->resolve(arg->args[1], diags);
                    return registry_.getMap(k, v);
                }
                if (name == "Option" && arg->args.size() == 1) {
                    TypeLowering lowering(registry_, sym_);
                    return lowering.getOption(this->resolve(arg->args[0], diags));
                }
                if (name == "Result" && arg->args.size() == 2) {
                    TypeLowering lowering(registry_, sym_);
                    return lowering.getResult(
                        this->resolve(arg->args[0], diags), this->resolve(arg->args[1], diags));
                }

                diags.error(arg->pos, std::format("unknown generic type construction '{}'", name));
                return registry_.getPrimitive(TypeKind::Unknown);
            } else if constexpr (std::is_same_v<T, ast::NamedTypeExpr*>) {
                if (!arg->name) {
                    diags.error(arg->pos, "anonymous named type expression");
                    return registry_.getPrimitive(TypeKind::Unknown);
                }
                std::string_view nameStr = sym_.resolve(arg->name->name);
                if (const Type* prim = resolvePrimitive(nameStr)) {
                    return prim;
                }
                if (scope_) {
                    if (const Type* customType = scope_->resolveType(arg->name->name)) {
                        return customType;
                    }
                }
                diags.error(arg->pos, std::format("unresolved type name '{}'", nameStr));
                return registry_.getPrimitive(TypeKind::Unknown);
            }
        },
        expr);
}
const Type* TypeResolver::resolvePrimitive(std::string_view name)
{
    if (name == "i8")
        return registry_.getPrimitive(TypeKind::I8);
    if (name == "i16")
        return registry_.getPrimitive(TypeKind::I16);
    if (name == "i32")
        return registry_.getPrimitive(TypeKind::I32);
    if (name == "i64" || name == "int")
        return registry_.getPrimitive(TypeKind::I64);
    if (name == "i128")
        return registry_.getPrimitive(TypeKind::I128);
    if (name == "u8")
        return registry_.getPrimitive(TypeKind::U8);
    if (name == "u16")
        return registry_.getPrimitive(TypeKind::U16);
    if (name == "u32")
        return registry_.getPrimitive(TypeKind::U32);
    if (name == "u64")
        return registry_.getPrimitive(TypeKind::U64);
    if (name == "u128")
        return registry_.getPrimitive(TypeKind::U128);
    if (name == "f32")
        return registry_.getPrimitive(TypeKind::F32);
    if (name == "f64")
        return registry_.getPrimitive(TypeKind::F64);
    if (name == "bool")
        return registry_.getPrimitive(TypeKind::Bool);
    if (name == "char")
        return registry_.getPrimitive(TypeKind::Char);
    if (name == "unit")
        return registry_.getPrimitive(TypeKind::Unit);
    if (name == "string")
        return registry_.getPrimitive(TypeKind::String);
    if (name == "ptr")
        return registry_.getPrimitive(TypeKind::Ptr);
    if (name == "any")
        return registry_.getPrimitive(TypeKind::Any);
    return nullptr;
}
}
