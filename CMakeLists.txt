cmake_minimum_required(VERSION 3.20)
project(MamlCompiler C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Standard output directories
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/bin)
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)
set(CMAKE_ARCHIVE_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)

# Allow 'include(Module)' to search the cmake/ directory
list(APPEND CMAKE_MODULE_PATH "${CMAKE_SOURCE_DIR}/cmake")

include(CompilerFlags)
include(Dependencies)
include(EmbedFile)

# =============================================================================
# Generate Embedded Stdlib Headers
# =============================================================================
embed_file(
    "${CMAKE_SOURCE_DIR}/stdlib/_runtime_externs.maml"
    "${CMAKE_BINARY_DIR}/generated/embedded_externs.hpp"
    "RUNTIME_EXTERNS_MAML"
    "maml::embedded"
)

embed_file(
    "${CMAKE_SOURCE_DIR}/stdlib/stdlib.maml"
    "${CMAKE_BINARY_DIR}/generated/embedded_stdlib.hpp"
    "STDLIB_MAML"
    "maml::embedded"
)

# =============================================================================
# Component Subdirectories
# =============================================================================
add_subdirectory(parser)
add_subdirectory(types)
add_subdirectory(sema)
add_subdirectory(mir)
add_subdirectory(passes)
add_subdirectory(backend)
add_subdirectory(runtime)

# =============================================================================
# Executable
# =============================================================================
add_executable(mamlc mamlc.cpp)
target_include_directories(mamlc PRIVATE ${CMAKE_BINARY_DIR}/generated)
target_link_libraries(mamlc PRIVATE maml_backend)

install(TARGETS mamlc RUNTIME DESTINATION bin)

include(CTest)
enable_testing()