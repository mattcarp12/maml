add_library(maml_compiler STATIC src/compiler.cpp)
target_include_directories(maml_compiler PUBLIC include)
# Compiler context now safely depends on everything below it
target_link_libraries(maml_compiler PUBLIC maml_sema maml_types maml_parser maml_core)