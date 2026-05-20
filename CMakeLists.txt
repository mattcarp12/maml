cmake_minimum_required(VERSION 3.14)

# Force CMake to use Clang for standard systems matching
set(CMAKE_C_COMPILER "clang")
set(CMAKE_CXX_COMPILER "clang++")
# set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -stdlib=libc++")
# set(CMAKE_EXE_LINKER_FLAGS "${CMAKE_EXE_LINKER_FLAGS} -stdlib=libc++")

project(maml-backend LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# =============================================================================
# 1. FIND & LINK NATIVE LLVM LIBRARIES
# =============================================================================
# Look for a system-wide LLVM installation (handles packages from apt llvm-20 etc.)
find_package(LLVM REQUIRED CONFIG)

message(STATUS "Found LLVM ${LLVM_PACKAGE_VERSION}")
message(STATUS "Using LLVMConfig.cmake found in: ${LLVM_DIR}")

# Expose LLVM include directories to the preprocessor compiler phase
include_directories(${LLVM_INCLUDE_DIRS})
add_definitions(${LLVM_DEFINITIONS})

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
# 3. BUILD THE BACKEND BINARY
# =============================================================================
add_executable(maml-backend backend/backend.cpp)

# Map our targeted LLVM components to their underlying library names
llvm_map_components_to_libnames(llvm_libs Core Support)

# Link both the JSON parser dependency and the LLVM system dependencies
target_link_libraries(maml-backend PRIVATE 
    nlohmann_json::nlohmann_json 
    ${llvm_libs}
)