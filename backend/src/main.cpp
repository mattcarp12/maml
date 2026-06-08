#include <llvm/IR/Verifier.h>
#include <llvm/Support/TargetSelect.h>

#include <fstream>
#include <iostream>

#include "CodegenContext.h"
#include "ProgramGenerator.h"

using json = nlohmann::json;

int main(int argc, char *argv[]) {
  // Initialize the native target so DataLayout and any target-dependent
  // queries in the backend reflect the actual host machine.
  llvm::InitializeNativeTarget();
  llvm::InitializeNativeTargetAsmPrinter();
  llvm::InitializeNativeTargetAsmParser();

  json ast;

  // 1. Check if a file was passed. If not, read from stdin.
  if (argc > 1) {
    std::ifstream f(argv[1]);
    if (!f.is_open()) {
      std::cerr << "Could not open target input JSON file.\n";
      return 1;
    }
    ast = json::parse(f);
  } else {
    // Read directly from standard input (piped from Go)
    ast = json::parse(std::cin);
  }

  maml::CodegenContext ctx("maml_core_module");
  maml::compileProgram(ctx, ast);

  if (ctx.Error.hasErrors()) {
    std::cerr << "\nBackend Error: Compilation aborted due to semantic errors.\n";
    return 1;
  }

  // if (llvm::verifyModule(*ctx.Module, &llvm::errs())) {
  //   std::cerr << "\nBackend Error: Internal generated LLVM IR module "
  //                "contains structural verification errors!\n";
  //   return 1;
  // }

  ctx.Module->print(llvm::outs(), nullptr);
  llvm::outs().flush();
  return 0;
}