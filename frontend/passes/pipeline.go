package passes

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/mir"
)

// PassConfig controls which passes are active.
type PassConfig struct {
	Prune       bool
	Liveness    bool
	Borrow      bool
	LinearLower bool
	TypeLower   bool
	DropInject  bool // New pass to handle deterministic destruction
}

// DefaultConfig returns the production configuration, now ARC-free.
func DefaultConfig() PassConfig {
	return PassConfig{
		Prune:       true,
		Liveness:    true,
		Borrow:      true,
		LinearLower: true,
		TypeLower:   true,
		DropInject:  true, // Enabled for deterministic memory safety
	}
}

// RunPasses executes the modern MIR optimization and analysis pipeline.
func RunPasses(g *mir.Graph, cfg PassConfig) []ast.CompileError {
	if g == nil {
		return nil
	}

	// [1] Prune dead blocks
	if cfg.Prune {
		Graph(g)
	}

	// [2] Backward dataflow liveness analysis
	var livenessRes *LivenessResult
	if cfg.Liveness {
		livenessRes = AnalyzeLiveness(g)
	}

	// [3] Inject deterministic Drop instructions
	if cfg.DropInject && livenessRes != nil {
		InjectDrops(g, livenessRes)
	}

	// [4] Forward dataflow borrow checker (XOR Mutability + NLL)
	var borrowErrs []ast.CompileError
	if cfg.Borrow && livenessRes != nil {
		borrowAnalyzer := New()
		borrowErrs = borrowAnalyzer.Analyze(g, livenessRes)
	}

	// [5] Desugar Linear Types (Own, MutBorrow, KeepAlive)
	if cfg.LinearLower {
		LowerLinearTypes(g)
	}

	// [6] Lower Types to physical byte offsets
	if cfg.TypeLower {
		LowerTypes(g)
	}

	return borrowErrs
}
