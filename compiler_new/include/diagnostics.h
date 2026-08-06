#pragma once
#include <cstdint>
#include <string>
#include <utility>
#include <vector>

namespace maml {

enum class DiagSeverity : uint8_t { Error, Warning, Note };

struct Diagnostic {
    DiagSeverity severity;
    std::string message;
    // TODO(you): add a SourceLoc field here once ast.h's location type is
    // shared, so diagnostics can point at the offending source range
    // instead of just a message.
};

// Diagnostics collects compile errors/warnings/notes instead of throwing
// exceptions for normal (user-caused) compile errors. Every pass should
// take a `Diagnostics&` (via CompilerContext) and call error()/warning()
// rather than asserting or throwing on bad user input. Asserts remain fine
// for genuine internal-invariant violations.
class Diagnostics {
public:
    void error(std::string message) { diags_.push_back({ DiagSeverity::Error, std::move(message) }); }
    void warning(std::string message) { diags_.push_back({ DiagSeverity::Warning, std::move(message) }); }
    void note(std::string message) { diags_.push_back({ DiagSeverity::Note, std::move(message) }); }

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
    std::vector<Diagnostic> diags_;
};

} // namespace maml
