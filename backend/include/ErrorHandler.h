#ifndef MAML_ERROR_HANDLER_H
#define MAML_ERROR_HANDLER_H

#include <nlohmann/json.hpp>
#include <string_view>

namespace maml {

class ErrorHandler {
  bool hasError = false;

public:
  void report(std::string_view message, const nlohmann::json &node = nullptr);
  void fatal(std::string_view message, const nlohmann::json &node = nullptr);
  bool hasErrors() const { return hasError; }
};

} // namespace maml

#endif // MAML_ERROR_HANDLER_H