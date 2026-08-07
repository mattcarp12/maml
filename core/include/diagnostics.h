#pragma once

#include "token.h"

#include <cstdint>
#include <format>
#include <ostream>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

namespace maml {

enum class DiagSeverity : uint8_t { Error, Warning, Note };

struct Diagnostic {
    DiagSeverity severity;
    std::string stage; // Compiler phase (e.g., "Parser", "Sema", "CodeGen")
    Position pos {};
    std::string message;

    [[nodiscard]] std::string toString() const
    {
        std::string_view sevStr;
        switch (severity) {
        case DiagSeverity::Error:
            sevStr = "Error";
            break;
        case DiagSeverity::Warning:
            sevStr = "Warning";
            break;
        case DiagSeverity::Note:
            sevStr = "Note";
            break;
        }

        std::string prefix
            = stage.empty() ? std::format("[{}]", sevStr) : std::format("[{} {}]", stage, sevStr);

        if (pos.filename.empty() && pos.line == 0) {
            return std::format("{} {}", prefix, message);
        }
        return std::format("{} {}:{}:{}: {}", prefix, pos.filename, pos.line, pos.col, message);
    }
};

inline std::ostream& operator<<(std::ostream& os, const Diagnostic& diag)
{
    os << diag.toString();
    return os;
}

class Diagnostics {
public:
    void setStage(std::string stage) { currentStage_ = std::move(stage); }
    [[nodiscard]] const std::string& getStage() const { return currentStage_; }

    // --- Positional diagnostics ---
    void error(Position pos, std::string message)
    {
        diags_.push_back({ DiagSeverity::Error, currentStage_, pos, std::move(message) });
    }
    void warning(Position pos, std::string message)
    {
        diags_.push_back({ DiagSeverity::Warning, currentStage_, pos, std::move(message) });
    }
    void note(Position pos, std::string message)
    {
        diags_.push_back({ DiagSeverity::Note, currentStage_, pos, std::move(message) });
    }

    // Variadic std::format ergonomics for positional errors
    template <typename... Args>
    void error(Position pos, std::format_string<Args...> fmt, Args&&... args)
    {
        error(pos, std::format(fmt, std::forward<Args>(args)...));
    }

    template <typename... Args>
    void warning(Position pos, std::format_string<Args...> fmt, Args&&... args)
    {
        warning(pos, std::format(fmt, std::forward<Args>(args)...));
    }

    template <typename... Args>
    void note(Position pos, std::format_string<Args...> fmt, Args&&... args)
    {
        note(pos, std::format(fmt, std::forward<Args>(args)...));
    }

    // --- Unpositioned diagnostics ---
    void error(std::string message)
    {
        diags_.push_back({ DiagSeverity::Error, currentStage_, {}, std::move(message) });
    }
    void warning(std::string message)
    {
        diags_.push_back({ DiagSeverity::Warning, currentStage_, {}, std::move(message) });
    }
    void note(std::string message)
    {
        diags_.push_back({ DiagSeverity::Note, currentStage_, {}, std::move(message) });
    }

    // Variadic std::format ergonomics for unpositioned errors
    template <typename... Args> void error(std::format_string<Args...> fmt, Args&&... args)
    {
        error(std::format(fmt, std::forward<Args>(args)...));
    }

    template <typename... Args> void warning(std::format_string<Args...> fmt, Args&&... args)
    {
        warning(std::format(fmt, std::forward<Args>(args)...));
    }

    template <typename... Args> void note(std::format_string<Args...> fmt, Args&&... args)
    {
        note(std::format(fmt, std::forward<Args>(args)...));
    }

    [[nodiscard]] bool hasErrors() const
    {
        for (const auto& d : diags_) {
            if (d.severity == DiagSeverity::Error)
                return true;
        }
        return false;
    }

    [[nodiscard]] const std::vector<Diagnostic>& all() const { return diags_; }

private:
    std::string currentStage_;
    std::vector<Diagnostic> diags_;
};

} // namespace maml