#include "TypeLowering.h"
#include <llvm/IR/DataLayout.h>
#include <llvm/IR/DerivedTypes.h>

namespace maml {

llvm::Type *llvmTypeFor(CodegenContext &ctx, const nlohmann::json &typeJson) {
  if (typeJson.is_null())
    return llvm::Type::getVoidTy(ctx.Context);

  std::string_view kind = typeJson["kind"].get<std::string_view>();
  std::string cacheKey =
      typeJson.dump(); // dump() necessitates a physical string

  if (ctx.TypeCache.find(cacheKey) != ctx.TypeCache.end()) {
    return ctx.TypeCache[cacheKey];
  }

  llvm::Type *resultType = nullptr;

  if (kind == "Int") {
    resultType = llvm::Type::getInt32Ty(ctx.Context);
  } else if (kind == "Bool") {
    resultType = llvm::Type::getInt1Ty(ctx.Context);
  } else if (kind == "Unit") {
    resultType = llvm::Type::getVoidTy(ctx.Context);
  } else if (kind == "String") {
    resultType = llvm::StructType::get(
        ctx.Context, {llvm::PointerType::getUnqual(ctx.Context),
                      llvm::Type::getInt32Ty(ctx.Context)});
  } else if (kind == "Task") {
    resultType = llvm::PointerType::getUnqual(ctx.Context);
  } else if (kind == "Struct") {
    std::string_view structName = typeJson["name"].get<std::string_view>();
    llvm::StringRef llName(structName.data(), structName.size());

    if (llvm::StructType *existing =
            llvm::StructType::getTypeByName(ctx.Module->getContext(), llName)) {
      return existing;
    }
    llvm::StructType *structType =
        llvm::StructType::create(ctx.Context, llName);
    ctx.TypeCache[cacheKey] = structType;

    if (typeJson.contains("fields") && typeJson["fields"].is_array()) {
      std::vector<llvm::Type *> fieldTypes;
      for (const auto &field : typeJson["fields"]) {
        fieldTypes.push_back(llvmTypeFor(ctx, field["type"]));
      }
      structType->setBody(fieldTypes);
    }
    return structType;
  } else if (kind == "Array") {
    resultType = llvm::ArrayType::get(llvmTypeFor(ctx, typeJson["base"]),
                                      typeJson["size"]);
  } else if (kind == "Slice" || kind == "Vector") {
    resultType = llvm::StructType::get(
        ctx.Context, {llvm::PointerType::getUnqual(ctx.Context),
                      llvm::PointerType::getUnqual(ctx.Context),
                      llvm::Type::getInt32Ty(ctx.Context),
                      llvm::Type::getInt32Ty(ctx.Context)});
  } else if (kind == "Map") {
    resultType = llvm::PointerType::getUnqual(ctx.Context);
  } else if (kind == "SumType") {
    llvm::DataLayout DL(ctx.Module.get());
    uint64_t maxPayloadSize = 0;

    for (const auto &variant : typeJson["variants"]) {
      std::vector<llvm::Type *> fieldTypes;
      for (const auto &field : variant["fields"]) {
        fieldTypes.push_back(llvmTypeFor(ctx, field["type"]));
      }
      if (!fieldTypes.empty()) {
        llvm::StructType *payloadStruct =
            llvm::StructType::get(ctx.Context, fieldTypes);
        uint64_t variantSize = DL.getTypeAllocSize(payloadStruct);
        if (variantSize > maxPayloadSize)
          maxPayloadSize = variantSize;
      }
    }
    if (maxPayloadSize == 0) {
      resultType = llvm::StructType::get(ctx.Context,
                                         llvm::Type::getInt32Ty(ctx.Context));
    } else {
      resultType = llvm::StructType::get(
          ctx.Context, {llvm::Type::getInt32Ty(ctx.Context),
                        llvm::ArrayType::get(llvm::Type::getInt8Ty(ctx.Context),
                                             maxPayloadSize)});
    }
  } else {
    resultType = llvm::Type::getInt32Ty(ctx.Context);
  }

  ctx.TypeCache[cacheKey] = resultType;
  return resultType;
}

} // namespace maml