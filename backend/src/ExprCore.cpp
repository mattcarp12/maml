#include "ExprGenerator.hpp"
#include "TypeLowering.hpp"
#include <llvm/IR/Constants.h>
#include <llvm/IR/GlobalVariable.h>

namespace maml {

template <class... Ts> struct overloaded : Ts... {
    using Ts::operator()...;
};
template <class... Ts> overloaded(Ts...) -> overloaded<Ts...>;

llvm::Value* evaluateValue(CodegenContext& ctx, const mir::Value& val)
{
    return std::visit(
        overloaded { [&](std::monostate) -> llvm::Value* { return nullptr; },
            [&](const mir::Register& arg) -> llvm::Value* {
                std::string_view nameStr = ctx.Sym.resolve(arg.name);

                if (nameStr == "null") {
                    return llvm::ConstantPointerNull::get(
                        llvm::PointerType::getUnqual(ctx.Context));
                }
                if (llvm::Function* func = ctx.Module->getFunction(nameStr)) {
                    return func;
                }

                llvm::Value* rawSym = ctx.resolveSymbol(arg.name);
                if (!rawSym) {
                    ctx.Error.fatal("Variable '" + std::string(nameStr)
                        + "' is not defined in the current scope.");
                    return nullptr;
                }

                // Distinguish between memory pointers and raw SSA values[cite: 3]
                if (llvm::isa<llvm::AllocaInst>(rawSym)) {
                    llvm::Value* basePtr = ctx.getMemoryBase(arg.name);
                    llvm::Type* ty = ctx.SymbolTypes[arg.name];

                    if (ty && ty->isArrayTy()) {
                        // Arrays evaluate to their memory pointer in MAML[cite: 3]
                        return basePtr;
                    }

                    // Safely load the full value from the memory address[cite: 3]
                    return ctx.Builder->CreateLoad(ty, basePtr, std::string(nameStr) + "_val");
                }

                return rawSym;
            },
            [&](const mir::IntConstant& arg) -> llvm::Value* {
                llvm::Type* targetTy = llvmTypeFor(ctx, arg.type);
                return llvm::ConstantInt::get(targetTy, arg.value);
            },
            [&](const mir::BoolConstant& arg) -> llvm::Value* {
                return llvm::ConstantInt::get(
                    llvm::Type::getInt1Ty(ctx.Context), arg.value ? 1 : 0);
            },
            [&](const mir::StringConstant& arg) -> llvm::Value* {
                // Because arg.value is a string_view, we construct a std::string or pass it
                // directly if supported
                llvm::Constant* strConst
                    = llvm::ConstantDataArray::getString(ctx.Context, arg.value, true);
                llvm::GlobalVariable* globalVar
                    = new llvm::GlobalVariable(*ctx.Module, strConst->getType(), true,
                        llvm::GlobalValue::PrivateLinkage, strConst, "str_lit");
                return ctx.Builder->CreatePointerCast(
                    globalVar, llvm::PointerType::getUnqual(ctx.Context));
            } },
        val);
}

llvm::Value* evaluateAddress(CodegenContext& ctx, const mir::Value& val)
{
    return std::visit(overloaded { [&](std::monostate) -> llvm::Value* { return nullptr; },
                          [&](const mir::Register& arg) -> llvm::Value* {
                              llvm::Value* rawSym = ctx.resolveSymbol(arg.name);
                              if (!rawSym) {
                                  ctx.Error.fatal("Variable '"
                                      + std::string(ctx.Sym.resolve(arg.name))
                                      + "' is not defined in the current scope.");
                                  return nullptr;
                              }
                              if (llvm::isa<llvm::AllocaInst>(rawSym)) {
                                  llvm::Type* ty = ctx.SymbolTypes[arg.name];
                                  if (ty && ty->isPointerTy()) {
                                      return ctx.Builder->CreateLoad(ty, rawSym,
                                          std::string(ctx.Sym.resolve(arg.name)) + "_addr");
                                  }
                                  return rawSym;
                              }
                              return rawSym;
                          },
                          [&](auto&) -> llvm::Value* {
                              ctx.Error.fatal("Cannot take the address of a non-identifier value.");
                              return nullptr;
                          } },
        val);
}

} // namespace maml