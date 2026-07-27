#pragma once
#include "cfg.h"
#include "sym.h"

namespace maml::passes {
void eliminateDeadFunctions(mir::Program* prog, SymbolTable& sym);
}