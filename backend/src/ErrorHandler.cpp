#include "ErrorHandler.h"
#include "llvm/ADT/StringRef.h"
#include "llvm/Support/ErrorHandling.h"
#include <iostream>

namespace maml {

void ErrorHandler::report(std::string_view message,
                          const nlohmann::json &node) {
  hasError = true;
  std::cerr << "Semantic Error: " << message << "\n";
  if (!node.is_null()) {
    if (node.contains("line")) {
      std::cerr << "  at line " << node["line"];
      if (node.contains("column")) {
        std::cerr << ", column " << node["column"];
      }
      std::cerr << "\n";
    }
  }
}

void ErrorHandler::fatal(std::string_view message, const nlohmann::json &node) {
  report(message, node);
  // Explicitly wrap the string_view in a StringRef
  llvm::report_fatal_error(llvm::StringRef(message.data(), message.size()));
}

} // namespace maml