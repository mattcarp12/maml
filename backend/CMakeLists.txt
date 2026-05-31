cmake_minimum_required(VERSION 3.14)

# Force CMake to use Clang for standard systems matching
set(CMAKE_C_COMPILER "clang")
set(CMAKE_CXX_COMPILER "clang++")

project(maml-backend LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# =============================================================================
# 1. FIND & LINK NATIVE LLVM LIBRARIES
# =============================================================================
find_package(LLVM REQUIRED CONFIG)

message(STATUS "Found LLVM ${LLVM_PACKAGE_VERSION}")
message(STATUS "Using LLVMConfig.cmake found in: ${LLVM_DIR}")

# Expose LLVM include directories to the preprocessor compiler phase
include_directories(${LLVM_INCLUDE_DIRS})
add_definitions(${LLVM_DEFINITIONS})

# Expose our local include directory for modular headers
include_directories(include)

# =============================================================================
# 2. AUTOMATE THE NLOHMANN/JSON DOWNLOAD
# =============================================================================
cmake_policy(SET CMP0135 NEW)
include(FetchContent)
FetchContent_Declare(
    json
    URL https://github.com/nlohmann/json/releases/download/v3.11.3/json.tar.xz
)
FetchContent_MakeAvailable(json)

# =============================================================================
# 3. BUILD THE MODULAR BACKEND BINARY
# =============================================================================
# Define source files explicitly to maintain clear modular compilation units
set(BACKEND_SOURCES
    src/CodegenContext.cpp
    src/TypeLowering.cpp
    src/ExprCore.cpp
    src/ExprOperators.cpp
    src/ExprContainers.cpp
    src/ExprCalls.cpp
    src/StmtGenerator.cpp
    src/ProgramGenerator.cpp
    src/ErrorHandler.cpp
    src/main.cpp
)

add_executable(maml-backend ${BACKEND_SOURCES})

llvm_map_components_to_libnames(llvm_libs
    Core
    Support
    Analysis
    native
)

# Link the specific list instead of ${LLVM_AVAILABLE_LIBS}
target_link_libraries(maml-backend PRIVATE
    nlohmann_json::nlohmann_json
    ${llvm_libs}
)