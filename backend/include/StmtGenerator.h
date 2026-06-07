#ifndef MAML_STMT_GENERATOR_H
#define MAML_STMT_GENERATOR_H

#include <nlohmann/json.hpp>

#include "CodegenContext.h"

namespace maml {

// Strongly-typed representation of MIR instruction operations
enum class MirOp {
  Unknown,
  TempDecl,
  Assign,
  StructInit,
  ArrayInit,
  SliceRead,
  FieldRead,
  IndexAssign,
  IndexRead,
  Copy,
  Move,
  Cast,
  LoadPtr,
  Store,
  RefAlloc,
  RefInc,
  RefDec,
  VariantDiscriminant,
  VariantRead,
  VariantInit,
  CoroPrologue,
  BinaryOp,
  UnaryOp,
  CallInst
};

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt);
void compileTerminator(CodegenContext &ctx, const nlohmann::json &term);

}  // namespace maml

#endif  // MAML_STMT_GENERATOR_H