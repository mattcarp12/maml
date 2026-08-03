add_library(maml_types STATIC
    src/types.cpp
    src/type_registry.cpp
)
target_include_directories(maml_types PUBLIC include)
target_link_libraries(maml_types PUBLIC maml_parser)