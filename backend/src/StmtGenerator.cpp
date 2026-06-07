#include "StmtGenerator.h"

#include <string_view>
#include <unordered_map>

#include "ExprGenerator.h"

namespace maml {

// Forward declarations of our new file-level category compilers
void compileMemoryOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt);
void compileContainerOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt);
void compileCompoundOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt);
void compileControlFlowOps(CodegenContext &ctx, MirOp op, const nlohmann::json &stmt);

static MirOp parseMirOp(std::string_view opStr) {
  static const std::unordered_map<std::string_view, MirOp> opMap = {
      {"temp_decl", MirOp::TempDecl},
      {"assign", MirOp::Assign},
      {"struct_init", MirOp::StructInit},
      {"array_init", MirOp::ArrayInit},
      {"slice_read", MirOp::SliceRead},
      {"field_read", MirOp::FieldRead},
      {"index_assign", MirOp::IndexAssign},
      {"index_read", MirOp::IndexRead},
      {"copy", MirOp::Copy},
      {"move", MirOp::Move},
      {"cast", MirOp::Cast},
      {"load_ptr", MirOp::LoadPtr},
      {"store", MirOp::Store},
      {"ref_alloc", MirOp::RefAlloc},
      {"ref_inc", MirOp::RefInc},
      {"ref_dec", MirOp::RefDec},
      {"variant_discriminant", MirOp::VariantDiscriminant},
      {"variant_read", MirOp::VariantRead},
      {"variant_init", MirOp::VariantInit},
      {"coro_prologue", MirOp::CoroPrologue},
      {"binary_op", MirOp::BinaryOp},
      {"unary_op", MirOp::UnaryOp},
      {"call_inst", MirOp::CallInst}};

  auto it = opMap.find(opStr);
  if (it != opMap.end()) return it->second;
  return MirOp::Unknown;
}

void compileStatement(CodegenContext &ctx, const nlohmann::json &stmt) {
  if (stmt.is_null()) return;

  std::string_view opStr = stmt.contains("op") ? stmt["op"].get<std::string_view>() : "unknown";
  MirOp op = parseMirOp(opStr);

  switch (op) {
    case MirOp::TempDecl:
    case MirOp::Assign:
    case MirOp::Copy:
    case MirOp::Move:
    case MirOp::LoadPtr:
    case MirOp::Store:
    case MirOp::RefAlloc:
    case MirOp::RefInc:
    case MirOp::RefDec:
    case MirOp::Cast:
      compileMemoryOps(ctx, op, stmt);
      break;

    case MirOp::ArrayInit:
    case MirOp::SliceRead:
    case MirOp::IndexRead:
    case MirOp::IndexAssign:
      compileContainerOps(ctx, op, stmt);
      break;

    case MirOp::StructInit:
    case MirOp::FieldRead:
    case MirOp::VariantDiscriminant:
    case MirOp::VariantRead:
    case MirOp::VariantInit:
      compileCompoundOps(ctx, op, stmt);
      break;

    case MirOp::BinaryOp:
    case MirOp::UnaryOp:
    case MirOp::CallInst:
      compileControlFlowOps(ctx, op, stmt);
      break;

    case MirOp::CoroPrologue:
      break;

    case MirOp::Unknown:
    default:
      ctx.Error.fatal("Unknown instruction op reached switch router: " + std::string(opStr), stmt);
      break;
  }
}

void compileTerminator(CodegenContext &ctx, const nlohmann::json &term) {
  if (term.is_null()) return;
  std::string_view op = term["op"].get<std::string_view>();

  if (op == "ret") {
    if (term.contains("value") && !term["value"].is_null()) {
      llvm::Value *retVal = evaluateExpression(ctx, term["value"]);
      ctx.Builder->CreateRet(retVal);
    } else {
      ctx.Builder->CreateRetVoid();
    }
  } else if (op == "br") {
    int target = std::stoi(term["target"].get<std::string>());
    ctx.Builder->CreateBr(ctx.Blocks[target]);
  } else if (op == "cond_br") {
    llvm::Value *condVal = evaluateExpression(ctx, term["condition"]);
    int trueTarget = std::stoi(term["true_target"].get<std::string>());
    int falseTarget = std::stoi(term["false_target"].get<std::string>());
    ctx.Builder->CreateCondBr(condVal, ctx.Blocks[trueTarget], ctx.Blocks[falseTarget]);
  } else if (op == "unreachable") {
    ctx.Builder->CreateUnreachable();
  } else {
    ctx.Error.fatal("Unknown terminator op: " + std::string(op), term);
  }
}

}  // namespace maml