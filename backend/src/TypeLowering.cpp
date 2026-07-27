#include "TypeLowering.hpp"
#include "abi_generated.hpp"
#include <llvm/IR/DataLayout.h>
#include <llvm/IR/DerivedTypes.h>

namespace maml {

llvm::Type* llvmTypeFor(CodegenContext& ctx, const types::Type* type)
{
    if (!type) {
        return llvm::Type::getVoidTy(ctx.Context);
    }

    switch (type->kind) {
    // --- Primitives ---
    case types::TypeKind::I8:
        return llvm::Type::getInt8Ty(ctx.Context);
    case types::TypeKind::I16:
        return llvm::Type::getInt16Ty(ctx.Context);
    case types::TypeKind::I32:
        return llvm::Type::getInt32Ty(ctx.Context);
    case types::TypeKind::I64:
        return llvm::Type::getInt64Ty(ctx.Context);
    case types::TypeKind::I128:
        return llvm::Type::getInt128Ty(ctx.Context);
    case types::TypeKind::U8:
        return llvm::Type::getInt8Ty(ctx.Context);
    case types::TypeKind::U16:
        return llvm::Type::getInt16Ty(ctx.Context);
    case types::TypeKind::U32:
        return llvm::Type::getInt32Ty(ctx.Context);
    case types::TypeKind::U64:
        return llvm::Type::getInt64Ty(ctx.Context);
    case types::TypeKind::U128:
        return llvm::Type::getInt128Ty(ctx.Context);

    case types::TypeKind::F32:
        return llvm::Type::getFloatTy(ctx.Context);
    case types::TypeKind::F64:
        return llvm::Type::getDoubleTy(ctx.Context);

    case types::TypeKind::Bool:
        return llvm::Type::getInt1Ty(ctx.Context);
    case types::TypeKind::Unit:
        return llvm::Type::getVoidTy(ctx.Context);
    case types::TypeKind::Char:
        return llvm::Type::getInt32Ty(ctx.Context);

    // Pointers and opaque references
    case types::TypeKind::Any:
    case types::TypeKind::Ptr:
    case types::TypeKind::Function:
    case types::TypeKind::Future:
        return llvm::PointerType::getUnqual(ctx.Context);

    case types::TypeKind::Unknown:
        ctx.Error.fatal("Unknown primitive type reached backend pipeline.");
        return nullptr;

    // --- Runtime compound types (Using ABI Generators) ---
    case types::TypeKind::String:
        return rt::getStringType(ctx.Context);
    case types::TypeKind::View:
        return rt::getViewType(ctx.Context);
    case types::TypeKind::Vector:
        return rt::getVectorType(ctx.Context);
    case types::TypeKind::Map:
        return rt::getMapType(ctx.Context);

    // --- Composites ---
    case types::TypeKind::Array: {
        const auto& payload = std::get<types::ArrayPayload>(type->payload);
        return llvm::ArrayType::get(llvmTypeFor(ctx, payload.base), payload.size);
    }

    case types::TypeKind::Struct: {
        const auto& payload = std::get<types::StructPayload>(type->payload);
        std::string structName = std::string(ctx.Sym.resolve(payload.name));

        llvm::StructType* existingST = llvm::StructType::getTypeByName(ctx.Context, structName);
        if (existingST && !existingST->isOpaque()) {
            return existingST;
        }

        std::vector<llvm::Type*> fieldTypes;
        fieldTypes.reserve(payload.fields.size());
        for (const auto& field : payload.fields) {
            fieldTypes.push_back(llvmTypeFor(ctx, field.type));
        }

        llvm::StructType* st
            = existingST ? existingST : llvm::StructType::create(ctx.Context, structName);
        st->setBody(fieldTypes, /*isPacked=*/false);
        return st;
    }

    case types::TypeKind::Sum: {
        const auto& payload = std::get<types::SumPayload>(type->payload);
        std::string baseName = std::string(ctx.Sym.resolve(payload.baseName));

        llvm::StructType* existingST = llvm::StructType::getTypeByName(ctx.Context, baseName);
        if (existingST && !existingST->isOpaque()) {
            return existingST;
        }

        uint64_t maxPayloadSize = 0;
        const llvm::DataLayout& DL = ctx.Module->getDataLayout();

        for (const auto& variant : payload.variants) {
            std::vector<llvm::Type*> payloadFields;
            for (auto tt : variant.tupleTypes) {
                payloadFields.push_back(llvmTypeFor(ctx, tt));
            }
            for (const auto& f : variant.fields) {
                payloadFields.push_back(llvmTypeFor(ctx, f.type));
            }

            if (!payloadFields.empty()) {
                llvm::StructType* variantTy
                    = llvm::StructType::get(ctx.Context, payloadFields, false);
                uint64_t variantSize = DL.getTypeAllocSize(variantTy);
                if (variantSize > maxPayloadSize)
                    maxPayloadSize = variantSize;
            }
        }

        uint64_t numBlocks = (maxPayloadSize + 7) / 8;
        llvm::Type* discrimTy = llvm::Type::getInt32Ty(ctx.Context);
        llvm::Type* payloadTy
            = llvm::ArrayType::get(llvm::Type::getInt64Ty(ctx.Context), numBlocks);

        llvm::StructType* st
            = existingST ? existingST : llvm::StructType::create(ctx.Context, baseName);
        st->setBody({ discrimTy, payloadTy }, /*isPacked=*/false);
        return st;
    }

    default:
        ctx.Error.fatal("Unhandled type kind in llvmTypeFor");
        return nullptr;
    }
}

llvm::Type* llvmLayoutTypeFor(CodegenContext& ctx, const types::Type* type)
{
    if (!type) {
        ctx.Error.fatal("llvmLayoutTypeFor: null type");
        return nullptr;
    }

    // Bypass the standard local-variable pointer resolution to fetch the raw StructType[cite: 3]
    switch (type->kind) {
    case types::TypeKind::Vector:
        return rt::getVectorType(ctx.Context);
    case types::TypeKind::Map:
        return rt::getMapType(ctx.Context);
    case types::TypeKind::View:
        return rt::getViewType(ctx.Context);
    default:
        return llvmTypeFor(ctx, type);
    }
}

} // namespace maml