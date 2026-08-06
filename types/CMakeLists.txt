add_library(maml_types STATIC
    src/types.cpp
    src/type_registry.cpp
    src/type_lowering.cpp
    src/type_resolver.cpp
)
target_include_directories(maml_types PUBLIC include)
target_link_libraries(maml_types PUBLIC maml_parser maml_core)