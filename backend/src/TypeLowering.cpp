#include "TypeLowering.h"

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/DerivedTypes.h>

#include <algorithm>
#include <numeric>
#include <string_view>
#include <unordered_map>

namespace maml {

// -----------------------------------------------------------------------------
// Private String-to-Enum Parser
// -----------------------------------------------------------------------------

static TypeKind parseTypeKind(std::string_view kindStr) {
  static const std::unordered_map<std::string_view, TypeKind> kindMap = {
      {"struct", TypeKind::Struct}, {"sum_type", TypeKind::SumType}, {"array", TypeKind::Array},
      {"vector", TypeKind::Vector}, {"view", TypeKind::View},        {"map", TypeKind::Map},
      {"future", TypeKind::Future}};

  auto it = kindMap.find(kindStr);
  if (it != kindMap.end()) {
    return it->second;
  }
  return TypeKind::Unknown;
}

// -----------------------------------------------------------------------------
// Private Sub-Routers for Type Mapping
// -----------------------------------------------------------------------------

static llvm::Type *lowerPrimitiveType(CodegenContext &ctx, std::string_view kind, const nlohmann::json &typeJson) {
  if (kind == "i32") {
    return llvm::Type::getInt32Ty(ctx.Context);
  } else if (kind == "i1") {
    return llvm::Type::getInt1Ty(ctx.Context);
  } else if (kind == "void") {
    return llvm::Type::getVoidTy(ctx.Context);
  } else if (kind == "ptr" || kind == "any") {
    return llvm::PointerType::getUnqual(ctx.Context);
  } else if (kind == "string") {
    return llvm::StructType::get(ctx.Context,
                                 {llvm::PointerType::getUnqual(ctx.Context), llvm::Type::getInt32Ty(ctx.Context)});
  }

  ctx.Error.fatal("Unknown primitive type descriptor: " + std::string(kind), typeJson);
  return nullptr;
}

static llvm::Type *lowerStructType(CodegenContext &ctx, const nlohmann::json &typeJson) {
  if (!typeJson.contains("fields") || typeJson["fields"].is_null()) {
    return llvm::PointerType::getUnqual(ctx.Context);
  }

  std::string structName = typeJson.value("name", "__anon_struct");

  llvm::StructType *existingST = llvm::StructType::getTypeByName(ctx.Context, structName);
  if (existingST && !existingST->isOpaque()) {
    return existingST;
  }

  const auto &fieldsJson = typeJson["fields"];

  std::vector<size_t> order(fieldsJson.size());
  std::iota(order.begin(), order.end(), 0);
  std::sort(order.begin(), order.end(),
            [&](size_t a, size_t b) { return fieldsJson[a]["index"].get<int>() < fieldsJson[b]["index"].get<int>(); });

  std::vector<llvm::Type *> fieldTypes;
  fieldTypes.reserve(fieldsJson.size());
  for (size_t i : order) {
    fieldTypes.push_back(llvmTypeFor(ctx, fieldsJson[i]["type"]));
  }

  llvm::StructType *st = existingST ? existingST : llvm::StructType::create(ctx.Context, structName);
  st->setBody(fieldTypes, /*isPacked=*/false);
  return st;
}

static llvm::Type *lowerSumType(CodegenContext &ctx, const nlohmann::json &typeJson) {
  std::string structName = typeJson.value("name", "__anon_sum_type");

  llvm::StructType *existingST = llvm::StructType::getTypeByName(ctx.Context, structName);
  if (existingST && !existingST->isOpaque()) {
    return existingST;
  }

  int totalSize = typeJson.value("size", 8);
  int payloadSize = totalSize > 4 ? totalSize - 4 : 0;
  int numBlocks = (payloadSize + 7) / 8;

  llvm::Type *discrimTy = llvm::Type::getInt32Ty(ctx.Context);
  llvm::Type *payloadTy = llvm::ArrayType::get(llvm::Type::getInt64Ty(ctx.Context), numBlocks);

  llvm::StructType *st = existingST ? existingST : llvm::StructType::create(ctx.Context, structName);
  st->setBody({discrimTy, payloadTy}, /*isPacked=*/false);
  return st;
}

static llvm::Type *lowerArrayType(CodegenContext &ctx, const nlohmann::json &typeJson) {
  if (!typeJson.contains("elem_type")) {
    ctx.Error.fatal("Array type missing explicit 'elem_type' metadata specification", typeJson);
    return llvm::Type::getVoidTy(ctx.Context);
  }
  llvm::Type *elemTy = llvmTypeFor(ctx, typeJson["elem_type"]);
  int size = typeJson.value("size", 0);
  return llvm::ArrayType::get(elemTy, size);
}

static llvm::Type *lowerDynamicContainerType(CodegenContext &ctx) { return llvm::PointerType::getUnqual(ctx.Context); }

static llvm::Type *lowerViewType(CodegenContext &ctx) {
  // Views remain lightweight stack-allocated fat-pointer value structs
  return llvm::StructType::get(ctx.Context, {
                                                llvm::PointerType::getUnqual(ctx.Context),  // field 0: raw_ptr
                                                llvm::PointerType::getUnqual(ctx.Context),  // field 1: data_ptr
                                                llvm::Type::getInt32Ty(ctx.Context),        // field 2: len
                                                llvm::Type::getInt32Ty(ctx.Context),        // field 3: cap
                                            });
}

// -----------------------------------------------------------------------------
// Public AST/MIR Type Router Entry Point
// -----------------------------------------------------------------------------

llvm::Type *llvmTypeFor(CodegenContext &ctx, const nlohmann::json &typeJson) {
  if (typeJson.is_null()) {
    return llvm::Type::getVoidTy(ctx.Context);
  }

  std::string cacheKey = typeJson.dump();
  auto cached = ctx.TypeCache.find(cacheKey);
  if (cached != ctx.TypeCache.end()) {
    return cached->second;
  }

  llvm::Type *resultType = nullptr;

  // 1. Primitive Strings Router
  if (typeJson.is_string()) {
    std::string_view kind = typeJson.get<std::string_view>();
    resultType = lowerPrimitiveType(ctx, kind, typeJson);
  }
  // 2. Compound Objects Router
  else if (typeJson.is_object()) {
    if (!typeJson.contains("kind")) {
      ctx.Error.fatal("Compound metadata object missing explicit 'kind' discriminator", typeJson);
      return llvm::Type::getVoidTy(ctx.Context);
    }

    std::string_view kindStr = typeJson["kind"].get<std::string_view>();
    TypeKind kind = parseTypeKind(kindStr);

    switch (kind) {
      case TypeKind::Struct:
        resultType = lowerStructType(ctx, typeJson);
        break;
      case TypeKind::SumType:
        resultType = lowerSumType(ctx, typeJson);
        break;
      case TypeKind::Array:
        resultType = lowerArrayType(ctx, typeJson);
        break;
      case TypeKind::Vector:
      case TypeKind::Map:
      case TypeKind::Future:
        resultType = lowerDynamicContainerType(ctx);
        break;
      case TypeKind::View:
        resultType = lowerViewType(ctx);
        break;
      case TypeKind::Unknown:
      default:
        ctx.Error.fatal("Unrecognized compound type definition kind reached switch: " + std::string(kindStr), typeJson);
        break;
    }
  } else {
    ctx.Error.fatal("Malformed type structure specification reached backend pipeline", typeJson);
  }

  if (resultType) {
    ctx.TypeCache[cacheKey] = resultType;
  }

  return resultType;
}

}  // namespace maml