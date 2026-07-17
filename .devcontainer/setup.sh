#!/bin/bash

sudo apt-get update 
sudo apt-get install -y \
    libzstd-dev \
    libedit-dev \
    libc++-dev \
    libc++abi-dev \
    ninja-build \
    gdb \
    ccache \
    musl-tools
go install golang.org/x/tools/cmd/stringer@latest
