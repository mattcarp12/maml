add_library(maml_parser STATIC
    src/lexer.cpp
    src/parser.cpp
)
target_include_directories(maml_parser PUBLIC include)