add_library(maml_sema STATIC
    src/analyzer.cpp
    src/hir_desugarer.cpp
)
target_include_directories(maml_sema PUBLIC include)
target_link_libraries(maml_sema PUBLIC maml_types)