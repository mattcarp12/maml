add_library(mamlrt STATIC
    src/memory.cpp
    src/string.cpp
    src/vec.cpp
    src/map.cpp
    src/io.cpp
    src/coro.cpp
    src/net.cpp
)
target_include_directories(mamlrt PRIVATE include)
target_compile_options(mamlrt PRIVATE
    -ffunction-sections
    -fdata-sections
    -fno-rtti
    -fno-exceptions
    -Werror=missing-declarations
    -fno-sanitize=address
)

set_target_properties(mamlrt PROPERTIES
    ARCHIVE_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/runtime/lib"
)