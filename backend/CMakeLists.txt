add_library(maml_backend STATIC
    src/CodegenContext.cpp
    src/CompoundOps.cpp
    src/ControlFlowOps.cpp
    src/ExprCore.cpp
    src/MemoryOps.cpp
    src/TypeLowering.cpp
    src/StmtGenerator.cpp
    src/ProgramGenerator.cpp
)
target_include_directories(maml_backend PUBLIC include ${LLVM_INCLUDE_DIRS})
target_compile_definitions(maml_backend PUBLIC ${LLVM_DEFINITIONS})
target_link_libraries(maml_backend PUBLIC maml_passes ${llvm_libs})