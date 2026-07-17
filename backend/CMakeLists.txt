cmake_minimum_required(VERSION 3.14)

project(maml-backend LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

if(CMAKE_BUILD_TYPE STREQUAL "Debug")
    add_compile_options(-fsanitize=address -g -O0)
    add_link_options(-fsanitize=address)
endif()

# Enable ccache if it is installed on the system
find_program(CCACHE_PROGRAM ccache)
if(CCACHE_PROGRAM)
    set(CMAKE_C_COMPILER_LAUNCHER   ${CCACHE_PROGRAM})
    set(CMAKE_CXX_COMPILER_LAUNCHER ${CCACHE_PROGRAM})
    message(STATUS "ccache enabled for faster recompilations.")
endif()


# =============================================================================
# 1. FIND & LINK NATIVE LLVM LIBRARIES
# =============================================================================
find_package(LLVM REQUIRED CONFIG)
message(STATUS "Found LLVM ${LLVM_PACKAGE_VERSION}")
message(STATUS "Using LLVMConfig.cmake found in: ${LLVM_DIR}")

include_directories(${LLVM_INCLUDE_DIRS})
add_definitions(${LLVM_DEFINITIONS})
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
set(BACKEND_SOURCES
    src/CodegenContext.cpp
    src/CompoundOps.cpp
    src/ControlFlowOps.cpp
    src/ExprCore.cpp
    src/MemoryOps.cpp
    src/TypeLowering.cpp
    src/StmtGenerator.cpp
    src/ProgramGenerator.cpp
    src/main.cpp
)

add_executable(maml-backend ${BACKEND_SOURCES})

llvm_map_components_to_libnames(llvm_libs
    Core
    Support
    Analysis
    native
)

target_link_libraries(maml-backend PRIVATE
    nlohmann_json::nlohmann_json
    ${llvm_libs}
)