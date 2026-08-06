add_library(maml_sema STATIC
    src/control_flow_pass.cpp
    src/declaration_pass.cpp
    src/desugar_pass.cpp
    src/semantic_analyzer.cpp
    src/type_check_pass.cpp
    src/type_resolution_pass.cpp
    src/scope.cpp
)
target_include_directories(maml_sema PUBLIC include)
target_link_libraries(maml_sema PUBLIC maml_compiler maml_types maml_parser maml_core)