package passes

import (
	"github.com/mattcarp12/maml/frontend/ast"
	"github.com/mattcarp12/maml/frontend/mir"
)

// PassConfig controls which passes are active. This lets you turn them on
// one at a time during development.
type PassConfig struct {
	Prune       bool
	Escape      bool
	AllocLower  bool
	Liveness    bool
	ARC         bool
	SROA        bool
	Borrow      bool
	LinearLower bool
	TypeLower   bool
}

// DefaultConfig returns the fully-enabled production configuration.
func DefaultConfig() PassConfig {
	return PassConfig{
		Prune:       true,
		Escape:      true,
		AllocLower:  true,
		Liveness:    true,
		ARC:         true,
		SROA:        false, // Left false per Phase 2 Roadmap safety guidelines
		Borrow:      true,
		LinearLower: true,
		TypeLower:   true,
	}
}

// RunPasses executes the MIR optimization and analysis pipeline
// for a single function's control flow graph.
// It returns any borrow-checker errors found.
func RunPasses(g *mir.Graph, cfg PassConfig) []ast.CompileError {
	if g == nil {
		return nil
	}

	// [1] Prune dead blocks before any analysis runs
	if cfg.Prune {
		Graph(g)
	}

	// [2] Fixed-point escape analysis
	var escapeMap map[string]EscapeState
	if cfg.Escape {
		escapeMap = AnalyzeEscape(g)
	}

	// [3] Allocation lowering based on heap promotion
	if cfg.AllocLower && escapeMap != nil {
		LowerAllocations(g, escapeMap)
	}

	// [4] Backward dataflow liveness analysis
	var livenessRes *LivenessResult
	if cfg.Liveness {
		livenessRes = AnalyzeLiveness(g)
	}

	// [5] Inject automated reference counting calls (retain/release)
	if cfg.ARC && livenessRes != nil {
		InjectARC(g, livenessRes, escapeMap)
	}

	// [6] SROA Optimization (Optional placeholder)
	if cfg.SROA {
		// If sroa.go implements Optimize(g), uncomment this call:
		// Optimize(g)
	}

	// [7] Forward dataflow borrow checker
	var borrowErrs []ast.CompileError
	if cfg.Borrow && livenessRes != nil {
		borrowAnalyzer := New()
		borrowErrs = borrowAnalyzer.Analyze(g, livenessRes)
	}

	// [8] Desugar Linear Types (Own, Freeze, MutBorrow, KeepAlive)
	// MUST run after Borrow and ARC, but before C++ backend export.
	if cfg.LinearLower {
		LowerLinearTypes(g)
	}

	// [9] Lower Types to physical byte offsets
	// MUST run near the end to ensure all temporary registers (including unrolled paths) are resolved.
	if cfg.TypeLower && escapeMap != nil {
		LowerTypes(g, escapeMap)
	}

	return borrowErrs
}
