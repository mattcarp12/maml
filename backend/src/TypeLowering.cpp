// =============================================================================
// backend/src/TypeLowering.cpp
// =============================================================================

#include "TypeLowering.hpp"

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/DerivedTypes.h>
#include <llvm/IR/Module.h>

#include "abi_generated.hpp"
#include "types_generated.hpp"

namespace maml {

llvm::Type* llvmTypeForVariant(CodegenContext& ctx, const maml::Type& generatedType);

struct TypeVisitor {
  CodegenContext& ctx;

  // --- Primitives ---
  llvm::Type* operator()(const I8Type&) { return llvm::Type::getInt8Ty(ctx.Context); }
  llvm::Type* operator()(const I16Type&) { return llvm::Type::getInt16Ty(ctx.Context); }
  llvm::Type* operator()(const I32Type&) { return llvm::Type::getInt32Ty(ctx.Context); }
  llvm::Type* operator()(const I64Type&) { return llvm::Type::getInt64Ty(ctx.Context); }
  llvm::Type* operator()(const I128Type&) { return llvm::Type::getInt128Ty(ctx.Context); }
  llvm::Type* operator()(const U8Type&) { return llvm::Type::getInt8Ty(ctx.Context); }
  llvm::Type* operator()(const U16Type&) { return llvm::Type::getInt16Ty(ctx.Context); }
  llvm::Type* operator()(const U32Type&) { return llvm::Type::getInt32Ty(ctx.Context); }
  llvm::Type* operator()(const U64Type&) { return llvm::Type::getInt64Ty(ctx.Context); }
  llvm::Type* operator()(const U128Type&) { return llvm::Type::getInt128Ty(ctx.Context); }

  llvm::Type* operator()(const F32Type&) { return llvm::Type::getFloatTy(ctx.Context); }
  llvm::Type* operator()(const F64Type&) { return llvm::Type::getDoubleTy(ctx.Context); }

  llvm::Type* operator()(const BoolType&) { return llvm::Type::getInt1Ty(ctx.Context); }
  llvm::Type* operator()(const UnitType&) { return llvm::Type::getVoidTy(ctx.Context); }
  llvm::Type* operator()(const AnyType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const PtrType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const CharType&) { return llvm::Type::getInt32Ty(ctx.Context); }
  llvm::Type* operator()(const FutureType&) { return llvm::PointerType::getUnqual(ctx.Context); }

  llvm::Type* operator()(const UnknownType&) {
    ctx.Error.fatal("Unknown primitive type reached backend pipeline.");
    return nullptr;
  }

  // --- Runtime compound types now use the generated, schema‑driven builders ---
  llvm::Type* operator()(const StringType&) { return rt::getStringType(ctx.Context); }
  llvm::Type* operator()(const ViewType&) { return rt::getViewType(ctx.Context); }
  llvm::Type* operator()(const VectorType&) { return rt::getVectorType(ctx.Context); }
  llvm::Type* operator()(const MapType&) { return rt::getMapType(ctx.Context); }
  llvm::Type* operator()(const RefType&) { return rt::getRefType(ctx.Context); }

  // --- Composites ---
  llvm::Type* operator()(const StructType& t) {
    llvm::StructType* existingST = llvm::StructType::getTypeByName(ctx.Context, t.name);
    if (existingST && !existingST->isOpaque()) {
      return existingST;
    }

    std::vector<llvm::Type*> fieldTypes;
    fieldTypes.reserve(t.fields.size());
    for (const auto& field : t.fields) {
      fieldTypes.push_back(llvmTypeForVariant(ctx, *field.type));
    }

    llvm::StructType* st = existingST ? existingST : llvm::StructType::create(ctx.Context, t.name);
    st->setBody(fieldTypes, /*isPacked=*/false);
    return st;
  }

  llvm::Type* operator()(const SumType& t) {
    llvm::StructType* existingST = llvm::StructType::getTypeByName(ctx.Context, t.base_name);
    if (existingST && !existingST->isOpaque()) {
      return existingST;
    }

    uint64_t maxPayloadSize = 0;
    const llvm::DataLayout& DL = ctx.Module->getDataLayout();

    for (const auto& variant : t.variants) {
      std::vector<llvm::Type*> payloadFields;
      for (const auto& f : variant.fields) {
        payloadFields.push_back(llvmTypeForVariant(ctx, *f.type));
      }
      for (const auto& tupleTy : variant.tuple_types) {
        payloadFields.push_back(llvmTypeForVariant(ctx, *tupleTy));
      }

      if (!payloadFields.empty()) {
        llvm::StructType* variantTy = llvm::StructType::get(ctx.Context, payloadFields, false);
        uint64_t variantSize = DL.getTypeAllocSize(variantTy);
        if (variantSize > maxPayloadSize) {
          maxPayloadSize = variantSize;
        }
      }
    }

    uint64_t numBlocks = (maxPayloadSize + 7) / 8;
    llvm::Type* discrimTy = llvm::Type::getInt32Ty(ctx.Context);
    llvm::Type* payloadTy = llvm::ArrayType::get(llvm::Type::getInt64Ty(ctx.Context), numBlocks);

    llvm::StructType* st = existingST ? existingST : llvm::StructType::create(ctx.Context, t.base_name);
    st->setBody({discrimTy, payloadTy}, /*isPacked=*/false);
    return st;
  }

  // --- Containers ---
  llvm::Type* operator()(const ArrayType& t) { return llvm::ArrayType::get(llvmTypeForVariant(ctx, *t.base), t.size); }

  // Removed hand‑crafted View, Vector, Map, Future, Ref entries — they are now above.
  // WeakRef, Sender, Receiver remain simple pointers (not part of the runtime schema yet)
  llvm::Type* operator()(const WeakRefType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const SenderType&) { return llvm::PointerType::getUnqual(ctx.Context); }
  llvm::Type* operator()(const ReceiverType&) { return llvm::PointerType::getUnqual(ctx.Context); }
};

llvm::Type* llvmTypeForVariant(CodegenContext& ctx, const maml::Type& generatedType) {
  return std::visit(TypeVisitor{ctx}, generatedType.inner);
}

llvm::Type* llvmTypeFor(CodegenContext& ctx, const std::shared_ptr<maml::Type>& type) {
  if (!type) {
    return llvm::Type::getVoidTy(ctx.Context);
  }
  return llvmTypeForVariant(ctx, *type);
}

llvm::Type* llvmLayoutTypeFor(CodegenContext& ctx, const std::shared_ptr<maml::Type>& type) {
  if (!type) {
    ctx.Error.fatal("llvmLayoutTypeFor: null type");
    return nullptr;
  }

  // Bypass the standard local-variable pointer resolution to fetch the raw StructType
  if (std::holds_alternative<maml::VectorType>(type->inner)) {
    return rt::getVectorType(ctx.Context);
  }
  if (std::holds_alternative<maml::MapType>(type->inner)) {
    return rt::getMapType(ctx.Context);
  }
  if (std::holds_alternative<maml::ViewType>(type->inner)) {
    return rt::getViewType(ctx.Context);
  }
  if (std::holds_alternative<maml::RefType>(type->inner)) {
    return rt::getRefType(ctx.Context);
  }

  // Fallback to standard type resolution for Arrays, Structs, etc.
  return llvmTypeFor(ctx, type);
}

}  // namespace maml