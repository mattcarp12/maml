#include <llvm/IR/Verifier.h>
#include <llvm/Support/TargetSelect.h>

#include <fstream>
#include <iostream>
#include <nlohmann/json.hpp>

#include "CodegenContext.hpp"
#include "ProgramGenerator.hpp"
#include "mir_generated.hpp"

using json = nlohmann::json;

int main(int argc, char *argv[]) {
  llvm::InitializeNativeTarget();
  llvm::InitializeNativeTargetAsmPrinter();
  llvm::InitializeNativeTargetAsmParser();

  json ast;
  if (argc > 1) {
    std::ifstream f(argv[1]);
    if (!f.is_open()) return 1;
    ast = json::parse(f);
  } else {
    ast = json::parse(std::cin);
  }

  // Parse the entire C++ strongly-typed AST instantly!
  mir::Program prog = ast.get<mir::Program>();

  maml::CodegenContext ctx("maml_core_module");
  maml::compileProgram(ctx, prog);

  if (ctx.Error.hasErrors()) {
    std::cerr << "\nBackend Error: Compilation aborted due to semantic errors.\n";
    return 1;
  }

  ctx.Module->print(llvm::outs(), nullptr);
  llvm::outs().flush();
  return 0;
}