cmake_minimum_required(VERSION 3.20)
project(MamlCompiler C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# Standard output directories
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/bin)
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)
set(CMAKE_ARCHIVE_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)

if(CMAKE_BUILD_TYPE STREQUAL "Debug")
    add_compile_options(-g -O0)
endif()

# =============================================================================
# 1. CCACHE
# =============================================================================
find_program(CCACHE_PROGRAM ccache)
if(CCACHE_PROGRAM)
    set(CMAKE_CXX_COMPILER_LAUNCHER "${CCACHE_PROGRAM}")
    set(CMAKE_C_COMPILER_LAUNCHER "${CCACHE_PROGRAM}")
endif()

# =============================================================================
# 2. LLVM
# =============================================================================
find_package(LLVM REQUIRED CONFIG)
message(STATUS "Found LLVM ${LLVM_PACKAGE_VERSION}")

# =============================================================================
# 3. COMPILER WARNINGS (applied to everything in this directory)
# =============================================================================
if(CMAKE_CXX_COMPILER_ID MATCHES "Clang|AppleClang|GNU")
    add_compile_options(-Wall -Wextra -pedantic -Wno-unused-parameter)
endif()

# =============================================================================
# 4. INTERNAL LIBRARIES
# =============================================================================

add_library(maml_parser STATIC
    parser/src/lexer.cpp
    parser/src/parser.cpp
)
target_include_directories(maml_parser PUBLIC parser/include)

add_library(maml_types STATIC
    types/src/types.cpp
    types/src/type_registry.cpp
)
target_include_directories(maml_types PUBLIC types/include)
target_link_libraries(maml_types PUBLIC maml_parser)

add_library(maml_sema STATIC
    sema/src/analyzer.cpp
    sema/src/scope.cpp
)
target_include_directories(maml_sema PUBLIC sema/include)
target_link_libraries(maml_sema PUBLIC maml_types)

add_library(maml_mir STATIC
    mir/src/cfg.cpp
    mir/src/builder_core.cpp
    mir/src/builder_lower_expr.cpp
    mir/src/builder_abi.cpp
)
target_include_directories(maml_mir PUBLIC mir/include)
target_link_libraries(maml_mir PUBLIC maml_sema)

add_library(maml_passes STATIC
    passes/src/borrow.cpp
    passes/src/drop_injector.cpp
    passes/src/linear_lower.cpp
    passes/src/liveness.cpp
    passes/src/pipeline.cpp
    passes/src/reachability.cpp
)
target_include_directories(maml_passes PUBLIC passes/include)
target_link_libraries(maml_passes PUBLIC maml_mir)

llvm_map_components_to_libnames(llvm_libs
    Core
    Support
    Analysis
    Coroutines
    native
)

add_library(maml_backend STATIC
    backend/src/CodegenContext.cpp
    backend/src/CompoundOps.cpp
    backend/src/ControlFlowOps.cpp
    backend/src/ExprCore.cpp
    backend/src/MemoryOps.cpp
    backend/src/TypeLowering.cpp
    backend/src/StmtGenerator.cpp
    backend/src/ProgramGenerator.cpp
)
target_include_directories(maml_backend PUBLIC backend/include ${LLVM_INCLUDE_DIRS})
target_compile_definitions(maml_backend PUBLIC ${LLVM_DEFINITIONS})
target_link_libraries(maml_backend PUBLIC maml_passes ${llvm_libs})

# =============================================================================
# 5. EXECUTABLE
# =============================================================================
add_executable(mamlc mamlc.cpp)
target_link_libraries(mamlc PRIVATE maml_backend)

# =============================================================================
# 6. RUNTIME LIBRARY
# =============================================================================
set(RUNTIME_SOURCES
    runtime/src/memory.cpp
    runtime/src/string.cpp
    runtime/src/vec.cpp
    runtime/src/map.cpp
    runtime/src/io.cpp
    runtime/src/coro.cpp
    runtime/src/net.cpp
)

add_library(mamlrt STATIC ${RUNTIME_SOURCES})
target_include_directories(mamlrt PRIVATE runtime/include)
target_compile_options(mamlrt PRIVATE
    -ffunction-sections
    -fdata-sections
    -fno-rtti
    -fno-exceptions
    -Werror=missing-declarations
)

# Match the path that mamlc expects at runtime
set_target_properties(mamlrt PROPERTIES
    ARCHIVE_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/runtime/lib"
)

# =============================================================================
# 7. INSTALL & TEST
# =============================================================================
install(TARGETS mamlc RUNTIME DESTINATION bin)

include(CTest)
enable_testing()