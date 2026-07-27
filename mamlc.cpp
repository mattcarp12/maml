#include <llvm/IR/Verifier.h>
#include <llvm/Support/FileSystem.h>
#include <llvm/Support/TargetSelect.h>
#include <llvm/Support/raw_ostream.h>

#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>

#include "CodegenContext.hpp"
#include "ProgramGenerator.hpp"
#include "analyzer.h"
#include "arena.h"
#include "builder.h"
#include "lexer.h"
#include "parser.h"
#include "pipeline.h"
#include "reachability.h"

static std::string readFile(const std::string& path)
{
    std::ifstream f(path);
    if (!f.is_open())
        return {};
    std::ostringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

static bool invokeClang(
    const std::string& llPath, const std::string& outPath, const std::string& runtimeLib)
{
    std::string cmd = "clang++ -Os --target=x86_64-linux-musl -static "
                      "-ffunction-sections -fdata-sections -fno-rtti -fno-exceptions "
                      "-flto -fuse-ld=lld -Wl,--gc-sections -Wl,-s "
                      "-Wno-override-module \""
        + llPath + "\" \"" + runtimeLib + "\" -o \"" + outPath + "\"";
    return std::system(cmd.c_str()) == 0;
}

// If you have a stdlib prelude file, read it here. Otherwise leave empty.
static std::string getStdlibPrelude() { return readFile("stdlib/prelude.maml"); }

int main(int argc, char* argv[])
{
    if (argc < 2) {
        std::cerr << "Usage: " << argv[0] << " <input.maml> [output]\n";
        return 1;
    }

    llvm::InitializeNativeTarget();
    llvm::InitializeNativeTargetAsmPrinter();
    llvm::InitializeNativeTargetAsmParser();

    std::string inputPath = argv[1];
    std::string outputPath = (argc > 2) ? argv[2] : "a.out";

    // -------------------------------------------------------------------------
    // 1. Read source + stdlib prelude
    // -------------------------------------------------------------------------
    std::string userSource = readFile(inputPath);
    if (userSource.empty()) {
        std::cerr << "Error: cannot read " << inputPath << "\n";
        return 1;
    }

    std::string sourceText = getStdlibPrelude() + "\n" + userSource;

    // -------------------------------------------------------------------------
    // 2. Lex & Parse
    // -------------------------------------------------------------------------
    maml::Arena arena;
    maml::SymbolTable sym;
    maml::types::TypeRegistry reg(arena);

    maml::Lexer lexer(sourceText);
    maml::Parser parser(lexer, sym, arena);
    auto astProg = parser.parseProgram();
    if (!parser.getErrors().empty()) {
        for (const auto& err : parser.getErrors()) {
            std::cerr << err << "\n";
        }
        return 1;
    }

    // -------------------------------------------------------------------------
    // 3. Semantic Analysis
    // -------------------------------------------------------------------------
    maml::sema::Analyzer analyzer(reg, sym);
    analyzer.analyze(astProg);
    auto semaErrors = analyzer.getErrors();
    if (!semaErrors.empty()) {
        for (const auto& err : semaErrors) {
            std::cerr << err << "\n";
        }
        return 1;
    }

    // -------------------------------------------------------------------------
    // 4. MIR Generation
    // -------------------------------------------------------------------------
    maml::mir::Builder builder(reg, sym);
    auto mirProg = builder.buildProgram(astProg);
    if (!mirProg) {
        std::cerr << "Error: MIR generation failed.\n";
        return 1;
    }

    // -------------------------------------------------------------------------
    // 5. MIR Passes (per-function)
    // -------------------------------------------------------------------------
    auto passesCfg = maml::passes::defaultConfig();
    std::vector<maml::ast::CompileError> allErrors;

    for (auto& fn : mirProg->functions) {
        if (fn.graph) {
            auto errs = maml::passes::runPasses(fn.graph.get(), fn.locals, sym, reg, passesCfg);
            allErrors.insert(allErrors.end(), errs.begin(), errs.end());
        }
    }

    if (!allErrors.empty()) {
        for (const auto& err : allErrors) {
            std::cerr << err << std::endl;
        }
        return 1;
    }

    // Strip unused functions (like Go's EliminateDeadFunctions)
    maml::passes::eliminateDeadFunctions(mirProg.get(), sym);

    // -------------------------------------------------------------------------
    // 6. LLVM Code Generation
    // -------------------------------------------------------------------------
    maml::CodegenContext ctx("maml_core_module", sym);
    maml::compileProgram(ctx, *mirProg);

    if (ctx.Error.hasErrors()) {
        std::cerr << "LLVM lowering errors.\n";
        return 1;
    }

    // -------------------------------------------------------------------------
    // 7. Write LLVM IR to temp file
    // -------------------------------------------------------------------------
    std::string tempDir = std::filesystem::temp_directory_path().string();
    std::string llPath = tempDir + "/maml_output.ll";

    std::error_code ec;
    llvm::raw_fd_ostream irStream(llPath, ec, llvm::sys::fs::OpenFlags::OF_Text);
    if (ec) {
        std::cerr << "Error writing IR: " << ec.message() << "\n";
        return 1;
    }
    ctx.Module->print(irStream, nullptr);
    irStream.flush();

    // -------------------------------------------------------------------------
    // 8. Invoke clang++ to link with runtime
    // -------------------------------------------------------------------------
    std::string runtimeLib = "./build/runtime/lib/libmamlrt.a";
    if (const char* root = std::getenv("MAML_ROOT")) {
        runtimeLib = std::string(root) + "/build/runtime/lib/libmamlrt.a";
    }

    if (!invokeClang(llPath, outputPath, runtimeLib)) {
        std::cerr << "Linking failed.\n";
        return 1;
    }

    std::cout << "✅ Build successful: " << outputPath << "\n";
    return 0;
}