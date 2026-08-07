#include "ast.h"
#include "cfg.h"
#include "sym.h"
#include "token.h"
#include "type_registry.h"
#include <llvm/IR/Verifier.h>
#include <llvm/Support/FileSystem.h>
#include <llvm/Support/ManagedStatic.h>
#include <llvm/Support/TargetSelect.h>
#include <llvm/Support/raw_ostream.h>

#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <memory>
#include <sstream>
#include <string>
#include <string_view>
#include <system_error>
#include <vector>

#ifdef __unix__
#include <sys/wait.h>
#endif

#include "CodegenContext.hpp"
#include "ProgramGenerator.hpp"
#include "arena.h"
#include "ast_printer.h"
#include "builder.h"
#include "compiler_context.h"
#include "embedded_externs.hpp"
#include "embedded_stdlib.hpp"
#include "lexer.h"
#include "mir_dump.h"
#include "parser.h"
#include "pipeline.h"
#include "reachability.h"
#include "semantic_analyzer.h"

// -----------------------------------------------------------------------------
// Driver Options
// -----------------------------------------------------------------------------
struct CompilerOptions {
    std::string inputPath;
    std::string outputPath = "a.out";
    bool runDirectly = false;

    bool dumpAst = false;
    std::string dumpAstPath = "maml_ast.txt";

    bool dumpIr = false;
    std::string dumpIrPath = "maml_llvmir.txt";

    bool dumpMir = false;
    std::string dumpMirPath = "maml_mir.txt";
};

// -----------------------------------------------------------------------------
// Utilities & Helper Logic
// -----------------------------------------------------------------------------
static std::string readFile(const std::string& path)
{
    std::ifstream f(path);
    if (!f.is_open())
        return {};
    std::ostringstream ss;
    ss << f.rdbuf();
    return ss.str();
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
              << "  -a, --dump-ast [file]    Dump generated AST (default: maml_ast.txt)\n"
              << "  -d, --dump-ir [file]    Dump generated LLVM IR (default: maml_dump.txt)\n"
              << "  --dump-mir [file]       Dump generated MIR (default: maml_mir.txt)\n"
              << "  -h, --help              Show usage information\n";
}

// -----------------------------------------------------------------------------
// Stage 1: CLI Argument Parsing
// -----------------------------------------------------------------------------
static bool parseCommandLine(int argc, char* argv[], CompilerOptions& opts)
{
    if (argc < 2) {
        printUsage(argv[0]);
        return false;
    }

    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];

        if (arg == "-h" || arg == "--help") {
            printUsage(argv[0]);
            return false;
        } else if (arg == "run" && i == 1) {
            opts.runDirectly = true;
        } else if (arg == "--run" || arg == "-r") {
            opts.runDirectly = true;
        } else if (arg == "--dump-ir" || arg == "-d") {
            opts.dumpIr = true;
            if (i + 1 < argc && argv[i + 1][0] != '-') {
                if (!(i + 2 == argc && opts.inputPath.empty())) {
                    opts.dumpIrPath = argv[++i];
                }
            }
        } else if (arg == "--dump-mir") {
            opts.dumpMir = true;
            if (i + 1 < argc && argv[i + 1][0] != '-') {
                if (!(i + 2 == argc && opts.inputPath.empty())) {
                    opts.dumpMirPath = argv[++i];
                }
            }
        } else if (arg == "--dump-ast" || arg == "-a") {
            opts.dumpAst = true;
            if (i + 1 < argc && argv[i + 1][0] != '-') {
                if (!(i + 2 == argc && opts.inputPath.empty())) {
                    opts.dumpAstPath = argv[++i];
                }
            }
        } else if (opts.inputPath.empty()) {
            opts.inputPath = arg;
        } else if (opts.outputPath == "a.out") {
            opts.outputPath = arg;
        }
    }

    if (opts.inputPath.empty()) {
        std::cerr << "Error: No input file specified.\n";
        printUsage(argv[0]);
        return false;
    }

    return true;
}

// -----------------------------------------------------------------------------
// Stage 2: Frontend & Middle-End (Lexing -> Parsing -> Sema -> MIR)
// -----------------------------------------------------------------------------
static std::unique_ptr<maml::mir::Program> runFrontendAndMiddleEnd(
    const CompilerOptions& opts, maml::CompilerContext& ctx)
{
    std::string userSource = readFile(opts.inputPath);
    if (userSource.empty()) {
        std::cerr << "Error: cannot read " << opts.inputPath << "\n";
        return nullptr;
    }

    // 1. Lexing
    std::vector<maml::Token> allTokens;
    auto lexFile = [&](std::string_view text, std::string_view filename) {
        maml::Lexer lexer(text, filename);
        maml::Token tok = lexer.nextToken();
        while (tok.type != maml::TokenType::END_OF_FILE) {
            allTokens.push_back(tok);
            tok = lexer.nextToken();
        }
    };

    lexFile(getExterns(), "_runtime_externs.maml");
    lexFile(getStdlib(), "stdlib.maml");
    lexFile(userSource, opts.inputPath);
    allTokens.push_back(
        { maml::TokenType::END_OF_FILE, "", { .filename = opts.inputPath, .line = 0, .col = 0 } });

    // 2. Parsing
    ctx.diagnostics.setStage("Parser");
    maml::Parser parser(allTokens, ctx.symbols, ctx.arena);
    auto astProg = parser.parseProgram();
    if (!parser.getErrors().empty()) {
        for (const auto& err : parser.getErrors()) {
            std::cerr << err << "\n";
        }
        return nullptr;
    }

    // 3. Semantic Analysis (5-Pass Pipeline)
    ctx.diagnostics.setStage("Sema");
    maml::sema::SemanticAnalyzer analyzer(ctx);
    if (!analyzer.analyze(astProg) || ctx.diagnostics.hasErrors()) {
        for (const auto& diag : ctx.diagnostics.all()) {
            std::cerr << diag << "\n";
        }
        return nullptr;
    }

    if (opts.dumpAst) {
        if (opts.dumpAstPath == "-") {
            maml::ast::AstPrinter printer(ctx.symbols, std::cout);
            printer.print(astProg);
        } else {
            std::ofstream astStream(opts.dumpAstPath);
            if (!astStream.is_open()) {
                std::cerr << "Error writing AST dump to " << opts.dumpAstPath << "\n";
            } else {
                maml::ast::AstPrinter printer(ctx.symbols, astStream);
                printer.print(astProg);
                std::cout << "📄 AST dumped to " << opts.dumpAstPath << "\n";
            }
        }
    }

    // 4. MIR Generation
    ctx.diagnostics.setStage("MIR");
    maml::mir::Builder builder(ctx);
    auto mirProg = builder.buildProgram(astProg);
    if (!mirProg) {
        std::cerr << "Error: MIR generation failed.\n";
        return nullptr;
    }

    // 5. MIR Optimization Passes
    auto passesCfg = maml::passes::defaultConfig();
    std::vector<maml::ast::CompileError> allErrors;

    for (auto& fn : mirProg->functions) {
        if (fn.graph) {
            auto errs = maml::passes::runPasses(
                fn.graph.get(), fn.locals, ctx.symbols, ctx.types.registry, passesCfg);
            allErrors.insert(allErrors.end(), errs.begin(), errs.end());
        }
    }

    if (!allErrors.empty()) {
        for (const auto& err : allErrors) {
            std::cerr << err << std::endl;
        }
        return nullptr;
    }

    maml::passes::eliminateDeadFunctions(mirProg.get(), ctx.symbols);

    // Optional MIR Dump
    if (opts.dumpMir) {
        if (opts.dumpMirPath == "-") {
            maml::mir::dumpProgramMIR(std::cout, *mirProg, ctx.symbols);
        } else {
            std::ofstream mirStream(opts.dumpMirPath);
            if (!mirStream.is_open()) {
                std::cerr << "Error writing MIR dump to " << opts.dumpMirPath << "\n";
            } else {
                maml::mir::dumpProgramMIR(mirStream, *mirProg, ctx.symbols);
                std::cout << "📄 MIR dumped to " << opts.dumpMirPath << "\n";
            }
        }
    }

    return mirProg;
}

// -----------------------------------------------------------------------------
// Stage 3: LLVM Backend Code Generation & Emission
// -----------------------------------------------------------------------------
static bool runBackendCodegen(const CompilerOptions& opts, maml::mir::Program& mirProg,
    maml::SymbolTable& sym, maml::types::TypeRegistry& typeRegistry, const std::string& llPath)
{
    maml::CodegenContext ctx("maml_core_module", sym, typeRegistry);
    maml::compileProgram(ctx, mirProg);

    if (ctx.Error.hasErrors()) {
        std::cerr << "LLVM lowering errors.\n";
        return false;
    }

    // Optional LLVM IR Dump
    if (opts.dumpIr) {
        std::error_code dumpEc;
        llvm::raw_fd_ostream dumpStream(opts.dumpIrPath, dumpEc, llvm::sys::fs::OpenFlags::OF_Text);
        if (dumpEc) {
            std::cerr << "Error writing IR dump to " << opts.dumpIrPath << ": " << dumpEc.message()
                      << "\n";
        } else {
            ctx.Module->print(dumpStream, nullptr);
            dumpStream.flush();
            std::cout << "📄 LLVM IR dumped to " << opts.dumpIrPath << "\n";
        }
    }

    // Output temporary IR file
    std::error_code ec;
    llvm::raw_fd_ostream irStream(llPath, ec, llvm::sys::fs::OpenFlags::OF_Text);
    if (ec) {
        std::cerr << "Error writing IR: " << ec.message() << "\n";
        return false;
    }
    ctx.Module->print(irStream, nullptr);
    irStream.flush();
    llvm::llvm_shutdown();
    return true;
}

// -----------------------------------------------------------------------------
// Stage 4: Subprocess Linker & Execution
// -----------------------------------------------------------------------------
static bool invokeClangOnLlvmIr(
    const std::string& llPath, const std::string& outPath, const std::string& runtimeLib)
{
    std::string cmd = "clang++ -Os --target=x86_64-linux-musl -static "
                      "-ffunction-sections -fdata-sections -fno-rtti -fno-exceptions "
                      "-flto -Wl,--gc-sections -Wl,-s "
                      "-Wno-override-module \""
        + llPath + "\" \"" + runtimeLib + "\" -o \"" + outPath + "\"";
    return std::system(cmd.c_str()) == 0;
}

static int linkAndExecute(const CompilerOptions& opts, const std::string& llPath)
{
    std::string runtimeLib = "./build/runtime/lib/libmamlrt.a";
    if (const char* root = std::getenv("MAML_ROOT")) {
        runtimeLib = std::string(root) + "/build/runtime/lib/libmamlrt.a";
    }

    std::string tempDir = std::filesystem::temp_directory_path().string();
    std::string targetExec = opts.runDirectly ? (tempDir + "/maml_run_bin") : opts.outputPath;

    if (!invokeClangOnLlvmIr(llPath, targetExec, runtimeLib)) {
        std::cerr << "Linking failed.\n";
        return 1;
    }

    if (opts.runDirectly) {
        int exitStatus = std::system(targetExec.c_str());
        std::filesystem::remove(targetExec);

#ifdef __unix__
        if (WIFEXITED(exitStatus)) {
            return WEXITSTATUS(exitStatus);
        }
#endif
        return exitStatus;
    }

    std::cout << "✅ Build successful: " << opts.outputPath << "\n";
    return 0;
}

// -----------------------------------------------------------------------------
// Main Entry Point
// -----------------------------------------------------------------------------
int main(int argc, char* argv[])
{
    CompilerOptions opts;
    if (!parseCommandLine(argc, argv, opts)) {
        return 1;
    }

    llvm::InitializeNativeTarget();
    llvm::InitializeNativeTargetAsmPrinter();
    llvm::InitializeNativeTargetAsmParser();

    maml::Arena arena;
    maml::SymbolTable sym;
    maml::CompilerContext ctx(arena, sym);

    // 1. AST & MIR Pipeline
    auto mirProg = runFrontendAndMiddleEnd(opts, ctx);
    if (!mirProg) {
        return 1;
    }

    // 2. LLVM IR Generation
    std::string tempDir = std::filesystem::temp_directory_path().string();
    std::string tempLlPath = tempDir + "/maml_output.ll";

    if (!runBackendCodegen(opts, *mirProg, ctx.symbols, ctx.types.registry, tempLlPath)) {
        return 1;
    }

    // 3. Clang Link & Run
    int exitCode = linkAndExecute(opts, tempLlPath);

    // Temp file cleanup
    std::filesystem::remove(tempLlPath);

    return exitCode;
}