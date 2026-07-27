#!/bin/bash

sudo apt-get update

# apt-get install -y --no-install-recommends \
#         clang-18 \
#         llvm-18 \
#         llvm-18-dev \
#         libclang-18-dev \
#         lldb-18 \
#     && ln -s /usr/bin/clang-18 /usr/bin/clang \
#     && ln -s /usr/bin/clang++-18 /usr/bin/clang++ \
#     && ln -s /usr/bin/llvm-config-18 /usr/bin/llvm-config \
#     && rm -rf /var/lib/apt/lists/*

sudo apt-get install -y \
    libzstd-dev \
    libedit-dev \
    libc++-dev \
    libc++abi-dev \
    ninja-build \
    gdb \
    ccache \
    musl-tools \
    clang-format \
    clang-tidy \
    valgrind \
    clangd \
    && rm -rf /var/lib/apt/lists/*

go install golang.org/x/tools/cmd/stringer@latest
