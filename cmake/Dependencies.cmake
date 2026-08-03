find_package(LLVM REQUIRED CONFIG)
message(STATUS "Found LLVM ${LLVM_PACKAGE_VERSION}")

llvm_map_components_to_libnames(llvm_libs
    Core
    Support
    Analysis
    Coroutines
    native
)