add_library(maml_mir STATIC
    src/builder_core.cpp
    src/builder_lower_expr.cpp
    src/builder_abi.cpp
)
target_include_directories(maml_mir PUBLIC include)
target_link_libraries(maml_mir PUBLIC maml_sema)