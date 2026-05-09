package codegen

import (
	"fmt"
)

// Validate performs a structural sanity check on the generated IR.
func (c *Codegen) Validate() error {
	for _, f := range c.module.Funcs {
		for _, block := range f.Blocks {
			// Rule: Every basic block MUST have a terminator (ret, br, condbr, etc.)
			if block.Term == nil {
				return fmt.Errorf("codegen error: block '%s' in function '%s' is missing a terminator",
					block.LocalName, f.Ident())
			}
		}
	}
	return nil
}
