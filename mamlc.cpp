#include "ast.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"
#include <llvm/IR/Verifier.h>
#include <llvm/Support/FileSystem.h>
#include <llvm/Support/TargetSelect.h>
#include <llvm/Support/raw_ostream.h>

#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <sstream>
#include <stdlib.h>
#include <string>
#include <string_view>
#include <system_error>
#include <vector>

#ifdef __unix__
#include <sys/wait.h>
#endif

#include "CodegenContext.hpp"
#include "ProgramGenerator.hpp"
#include "analyzer.h"
#include "arena.h"
#include "builder.h"
#include "embedded_externs.hpp"
#include "embedded_stdlib.hpp" 
#include "lexer.h"
#include "mir_dump.h"
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
                      "-flto -Wl,--gc-sections -Wl,-s "
                      "-Wno-override-module \""
        + llPath + "\" \"" + runtimeLib + "\" -o \"" + outPath + "\"";
    return std::system(cmd.c_str()) == 0;
}

static std::string_view getStdlib() { return maml::embedded::STDLIB_MAML; }
static std::string_view getExterns() { return maml::embedded::RUNTIME_EXTERNS_MAML; }

static void printUsage(const char* progName)
{
    std::cout << "Usage:\n"
              << "  " << progName << " [options] <input.maml> [output]\n"
              << "  " << progName << " run <input.maml>\n\n"
              << "Options:\n"
              << "  -r, --run               Compile and execute immediately\n"
              << "  -d, --dump-ir [file]    Dump generated LLVM IR (default: maml_dump.txt)\n"
              << "  --dump-mir [file]       Dump generated MIR (default: maml_mir.txt)\n"
              << "  -h, --help              Show usage information\n";
}

int main(int argc, char* argv[])
{
    if (argc < 2) {
        printUsage(argv[0]);
        return 1;
    }

    bool runDirectly = false;
    bool dumpIr = false;
    std::string dumpIrPath = "maml_dump.txt";
    std::string inputPath = "";
    std::string outputPath = "a.out";
    bool dumpMir = false;
    std::string dumpMirPath = "maml_mir.txt";

    // -------------------------------------------------------------------------
    // CLI Argument Parsing
    // -------------------------------------------------------------------------
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];

        if (arg == "-h" || arg == "--help") {
            printUsage(argv[0]);
            return 0;
        } else if (arg == "run" && i == 1) {
            runDirectly = true;
        } else if (arg == "--run" || arg == "-r") {
            runDirectly = true;
        } else if (arg == "--dump-ir" || arg == "-d") {
            dumpIr = true;
            if (i + 1 < argc && argv[i + 1][0] != '-') {
                if (i + 2 == argc && inputPath.empty()) {
                    // Next arg is the last arg, leave it alone for inputPath
                } else {
                    dumpIrPath = argv[++i];
                }
            }
        } else if (arg == "--dump-mir") {
            dumpMir = true;
            if (i + 1 < argc && argv[i + 1][0] != '-') {
                if (i + 2 == argc && inputPath.empty()) {
                    // Next arg is the last arg, leave it alone for inputPath
                } else {
                    dumpMirPath = argv[++i];
                }
            }
        } else if (inputPath.empty()) {
            inputPath = arg;
        } else if (outputPath == "a.out") {
            outputPath = arg;
        }
    }

    if (inputPath.empty()) {
        std::cerr << "Error: No input file specified.\n";
        printUsage(argv[0]);
        return 1;
    }

    llvm::InitializeNativeTarget();
    llvm::InitializeNativeTargetAsmPrinter();
    llvm::InitializeNativeTargetAsmParser();

    // -------------------------------------------------------------------------
    // 1. Read source & access embedded prelude
    // -------------------------------------------------------------------------
    std::string userSource = readFile(inputPath);
    if (userSource.empty()) {
        std::cerr << "Error: cannot read " << inputPath << "\n";
        return 1;
    }

    std::string_view externsSrc = getExterns();
    std::string_view stdlibSrc = getStdlib();

    // -------------------------------------------------------------------------
    // 2. Lex & Parse
    // -------------------------------------------------------------------------
    maml::Arena arena;
    maml::SymbolTable sym;
    maml::types::TypeRegistry reg(arena);

    std::vector<maml::Token> allTokens;

    auto lexFile = [&](std::string_view text, std::string_view filename) {
        maml::Lexer lexer(text, filename);
        maml::Token tok = lexer.nextToken();
        while (tok.type != maml::TokenType::END_OF_FILE) {
            allTokens.push_back(tok);
            tok = lexer.nextToken();
        }
    };

    lexFile(externsSrc, "_runtime_externs.maml");
    lexFile(stdlibSrc, "stdlib.maml");
    lexFile(userSource, inputPath);

    allTokens.push_back({ maml::TokenType::END_OF_FILE, "", { inputPath, 0, 0 } });

    maml::Parser parser(allTokens, sym, arena);
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

    maml::passes::eliminateDeadFunctions(mirProg.get(), sym);

    // -------------------------------------------------------------------------
    // Optional: Dump MIR to File or Terminal
    // -------------------------------------------------------------------------
    if (dumpMir) {
        if (dumpMirPath == "-") {
            maml::mir::dumpProgramMIR(std::cout, *mirProg, sym);
        } else {
            std::ofstream mirStream(dumpMirPath);
            if (!mirStream.is_open()) {
                std::cerr << "Error writing MIR dump to " << dumpMirPath << "\n";
            } else {
                maml::mir::dumpProgramMIR(mirStream, *mirProg, sym);
                std::cout << "📄 MIR dumped to " << dumpMirPath << "\n";
            }
        }
    }

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
    // Optional: Dump LLVM IR to File
    // -------------------------------------------------------------------------
    if (dumpIr) {
        std::error_code dumpEc;
        llvm::raw_fd_ostream dumpStream(dumpIrPath, dumpEc, llvm::sys::fs::OpenFlags::OF_Text);
        if (dumpEc) {
            std::cerr << "Error writing IR dump to " << dumpIrPath << ": " << dumpEc.message()
                      << "\n";
        } else {
            ctx.Module->print(dumpStream, nullptr);
            dumpStream.flush();
            std::cout << "📄 LLVM IR dumped to " << dumpIrPath << "\n";
        }
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

    std::string targetExec = runDirectly ? (tempDir + "/maml_run_bin") : outputPath;

    if (!invokeClang(llPath, targetExec, runtimeLib)) {
        std::cerr << "Linking failed.\n";
        return 1;
    }

    // -------------------------------------------------------------------------
    // 9. Execute directly if requested
    // -------------------------------------------------------------------------
    if (runDirectly) {
        int exitStatus = std::system(targetExec.c_str());
        std::filesystem::remove(targetExec);

#ifdef __unix__
        if (WIFEXITED(exitStatus)) {
            return WEXITSTATUS(exitStatus);
        }
#endif
        return exitStatus;
    }

    std::cout << "✅ Build successful: " << outputPath << "\n";
    return 0;
}