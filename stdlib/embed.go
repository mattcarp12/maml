package stdlib

import _ "embed"

//go:embed _runtime_externs.maml
var RuntimeExterns string

//go:embed stdlib.maml
var CoreStdlib string

// Prelude is the combined standard library injected into every MAML program.
var Prelude = RuntimeExterns + "\n" + CoreStdlib
