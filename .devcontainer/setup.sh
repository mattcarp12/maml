#!/bin/bash

sudo apt-get update 
sudo apt-get install -y libzstd-dev libedit-dev libc++-dev libc++abi-dev
go install golang.org/x/tools/cmd/stringer@latest
