cmake_minimum_required(VERSION 3.24)
project(maml-runtime LANGUAGES C CXX)

find_program(CCACHE_PROGRAM ccache)
if(CCACHE_PROGRAM)
    set(CMAKE_C_COMPILER_LAUNCHER   ${CCACHE_PROGRAM})
    set(CMAKE_CXX_COMPILER_LAUNCHER ${CCACHE_PROGRAM})
    message(STATUS "ccache enabled for faster recompilations.")
endif()


# Use modern C++ standard
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)


# Create the runtime static library 
set(RUNTIME_SOURCES
    src/memory.cpp
    src/string.cpp
    src/vec.cpp
    src/map.cpp
    src/io.cpp
    src/coro.cpp
    src/net.cpp
)

set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -ffunction-sections -fdata-sections -fno-rtti -fno-exceptions")
set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -ffunction-sections -fdata-sections")
add_library(mamlrt STATIC ${RUNTIME_SOURCES})
target_include_directories(mamlrt PRIVATE include)
set_target_properties(mamlrt PROPERTIES
    ARCHIVE_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/lib"
)
target_compile_options(mamlrt PRIVATE -Werror=missing-declarations)