type expr = Int of int
type func = Function of (string * expr)
type program = Program of func
