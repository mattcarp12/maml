#include <array>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <regex>
#include <sstream>
#include <string>
#include <vector>

#ifdef __unix__
#include <sys/wait.h>
#endif

namespace fs = std::filesystem;

// --- Regex Patterns ---
const std::regex skipRegex(R"(//\s*SKIP:\s*true)");
const std::regex exitCodeRegex(R"(//\s*EXIT_CODE:\s*(\d+))");
const std::regex expectedOutStart(R"(//\s*EXPECTED_OUTPUT:)");
const std::regex expectedErrRegex(R"(//\s*EXPECTED_ERROR:\s*(.+))");

struct ExecResult {
    int exitCode;
    std::string output;
};

struct FailureDetail {
    std::string testName;
    std::string reason;
};

// --- Helper: Command Execution ---
ExecResult runCommand(const std::string& cmd)
{
    std::array<char, 256> buffer;
    std::string output;

    FILE* pipe = popen((cmd + " 2>&1").c_str(), "r");
    if (!pipe) {
        throw std::runtime_error("popen() failed!");
    }

    while (fgets(buffer.data(), buffer.size(), pipe) != nullptr) {
        output += buffer.data();
    }

    int status = pclose(pipe);
    int exitCode = status;

#ifdef __unix__
    if (WIFEXITED(status)) {
        exitCode = WEXITSTATUS(status);
    }
#endif

    return { exitCode, output };
}

// --- Helper: Read File ---
std::string readFile(const std::string& path)
{
    std::ifstream f(path);
    if (!f.is_open())
        return "";
    std::ostringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

// --- Helper: String Trim ---
std::string trim(const std::string& s)
{
    auto start = s.find_first_not_of(" \t\r\n");
    if (start == std::string::npos)
        return "";
    auto end = s.find_last_not_of(" \t\r\n");
    return s.substr(start, end - start + 1);
}

// --- Parsers ---
bool shouldSkip(const std::string& src) { return std::regex_search(src, skipRegex); }

int parseExitCode(const std::string& src)
{
    std::smatch match;
    if (std::regex_search(src, match, exitCodeRegex) && match.size() > 1) {
        return std::stoi(trim(match[1].str()));
    }
    return 0;
}

std::string parseExpectedError(const std::string& src)
{
    std::smatch match;
    if (std::regex_search(src, match, expectedErrRegex) && match.size() > 1) {
        return trim(match[1].str());
    }
    return "";
}

std::string parseExpectedOutput(const std::string& src)
{
    std::istringstream stream(src);
    std::string line;
    std::vector<std::string> outputLines;
    bool inOutput = false;

    while (std::getline(stream, line)) {
        std::string trimmed = trim(line);

        if (std::regex_search(trimmed, expectedOutStart)) {
            inOutput = true;
            auto pos = trimmed.find("EXPECTED_OUTPUT:");
            if (pos != std::string::npos) {
                std::string rest = trim(trimmed.substr(pos + 16));
                if (!rest.empty()) {
                    outputLines.push_back(rest);
                }
            }
            continue;
        }

        if (inOutput) {
            if (trimmed.rfind("//", 0) == 0) {
                std::string clean = trim(trimmed.substr(2));
                if (!clean.empty()) {
                    outputLines.push_back(clean);
                }
            } else {
                break;
            }
        }
    }

    std::string result;
    for (size_t i = 0; i < outputLines.size(); ++i) {
        result += outputLines[i] + (i < outputLines.size() - 1 ? "\n" : "");
    }
    return result;
}

int main()
{
    std::string programsDir = "programs";
    if (!fs::exists(programsDir) || !fs::is_directory(programsDir)) {
        std::cerr << "Error: 'programs' directory not found.\n";
        return 1;
    }

    const char* rootEnv = std::getenv("MAML_ROOT");
    std::string mamlRoot = rootEnv ? rootEnv : "../..";
    std::string mamlBin = fs::absolute(mamlRoot + "/build/bin/mamlc").string();

    if (!fs::exists(mamlBin)) {
        std::cerr << "Error: maml binary not found at " << mamlBin << ". Build it first.\n";
        return 1;
    }

    int passed = 0;
    int skipped = 0;
    std::vector<FailureDetail> failures;
    std::string tempDir = fs::temp_directory_path().string();

    for (const auto& entry : fs::directory_iterator(programsDir)) {
        if (entry.path().extension() != ".maml")
            continue;

        std::string srcPath = entry.path().string();
        std::string testName = entry.path().stem().string();
        std::string fileName = entry.path().filename().string();
        std::string src = readFile(srcPath);

        if (shouldSkip(src)) {
            std::cout << "[ SKIP ]  " << fileName << "\n";
            skipped++;
            continue;
        }

        int expectedExit = parseExitCode(src);
        std::string expectedOut = parseExpectedOutput(src);
        std::string expectedErr = parseExpectedError(src);

        std::string binPath = tempDir + "/maml_bin_" + testName;

        std::string compileCmd = mamlBin + " " + srcPath + " " + binPath;
        ExecResult compileRes = runCommand(compileCmd);

        // Expected Compilation Error Check
        if (!expectedErr.empty()) {
            if (compileRes.exitCode == 0) {
                std::cout << "[ FAIL ]  " << fileName << "\n";
                failures.push_back({ fileName, "Expected compilation to fail, but it succeeded." });
                continue;
            }
            if (compileRes.output.find(expectedErr) == std::string::npos) {
                std::cout << "[ FAIL ]  " << fileName << "\n";
                std::string msg = "Expected error string missing.\n"
                                  "  Expected: "
                    + expectedErr
                    + "\n"
                      "  Actual:   "
                    + trim(compileRes.output);
                failures.push_back({ fileName, msg });
                continue;
            }
            std::cout << "[ PASS ]  " << fileName << "\n";
            passed++;
            continue;
        }

        // Unexpected Compilation Error Check
        if (compileRes.exitCode != 0) {
            std::cout << "[ FAIL ]  " << fileName << "\n";
            failures.push_back({ fileName,
                "Compilation failed unexpectedly.\n  Output: " + trim(compileRes.output) });
            continue;
        }

        ExecResult runRes = runCommand(binPath);
        fs::remove(binPath);

        // Exit Code Check
        if (runRes.exitCode != expectedExit) {
            std::cout << "[ FAIL ]  " << fileName << "\n";
            std::string msg = "Expected exit code: " + std::to_string(expectedExit)
                + "\n"
                  "  Actual exit code:   "
                + std::to_string(runRes.exitCode);
            failures.push_back({ fileName, msg });
            continue;
        }

        // Output Match Check
        if (!expectedOut.empty()) {
            std::string actualOutTrimmed = trim(runRes.output);
            if (expectedOut != actualOutTrimmed) {
                std::cout << "[ FAIL ]  " << fileName << "\n";
                std::string msg = "Expected output: '" + expectedOut
                    + "'\n"
                      "  Actual output:   '"
                    + actualOutTrimmed + "'";
                failures.push_back({ fileName, msg });
                continue;
            }
        }

        std::cout << "[ PASS ]  " << fileName << "\n";
        passed++;
    }

    // --- Failures Summary Section ---
    if (!failures.empty()) {
        std::cout << "\n=================================================================\n"
                  << "                     FAILURES SUMMARY (" << failures.size() << ")\n"
                  << "=================================================================\n";
        for (const auto& fail : failures) {
            std::cout << "\n● " << fail.testName << "\n"
                      << "  " << fail.reason << "\n";
        }
    }

    // --- Test Counts Summary ---
    std::cout << "\n-----------------------------------------------------------------\n"
              << "Passed:  " << passed << "\n"
              << "Failed:  " << failures.size() << "\n"
              << "Skipped: " << skipped << "\n"
              << "-----------------------------------------------------------------\n";

    return failures.empty() ? 0 : 1;
}