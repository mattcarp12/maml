add_library(maml_passes STATIC
    src/borrow.cpp
    src/drop_injector.cpp
    src/linear_lower.cpp
    src/liveness.cpp
    src/pipeline.cpp
    src/reachability.cpp
)
target_include_directories(maml_passes PUBLIC include)
target_link_libraries(maml_passes PUBLIC maml_mir)