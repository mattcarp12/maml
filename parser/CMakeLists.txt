add_library(maml_parser STATIC 
    src/parser.cpp
    src/lexer.cpp
    src/ast_printer.cpp
    src/token.cpp
)
target_include_directories(maml_parser PUBLIC include)
target_link_libraries(maml_parser PUBLIC maml_core)