package passes

import (
	"github.com/mattcarp12/maml/frontend/mir"
	"github.com/mattcarp12/maml/frontend/parser/ast"
	"github.com/mattcarp12/maml/frontend/types"
)

type PassConfig struct {
	Liveness    bool
	Borrow      bool
	LinearLower bool
	DropInject  bool
}

// DefaultConfig returns the production configuration, now ARC-free.
func DefaultConfig() PassConfig {
	return PassConfig{
		Liveness:    true,
		Borrow:      true,
		LinearLower: true,
		DropInject:  true,
	}
}

func RunPasses(g *mir.Graph, locals map[string]types.Type, cfg PassConfig) []ast.CompileError {
	if g == nil {
		return nil
	}

	var livenessRes *LivenessResult
	if cfg.Liveness {
		livenessRes = AnalyzeLiveness(g, locals)
	}

	var borrowErrs []ast.CompileError
	if cfg.Borrow && livenessRes != nil {
		borrowAnalyzer := New()
		borrowErrs = borrowAnalyzer.Analyze(g, locals, livenessRes)
	}

	if cfg.DropInject && livenessRes != nil {
		InjectDrops(g, locals, livenessRes)
	}

	if cfg.LinearLower {
		LowerLinearTypes(g)
	}

	return borrowErrs
}
