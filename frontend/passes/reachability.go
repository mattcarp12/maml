package passes

import "github.com/mattcarp12/maml/frontend/mir"

func EliminateDeadFunctions(prog *mir.Program) {
	if prog == nil {
		return
	}

	liveSet := make(map[string]bool)
	var worklist []string

	// 1. Mark entry points
	entryPoints := []string{"main",
		"maml_coro_runtime_init",
		"maml_alloc",
		"maml_free",
	}
	for _, ep := range entryPoints {
		worklist = append(worklist, ep)
		liveSet[ep] = true
	}

	// Helper to find a function by name
	getFunc := func(name string) *mir.Function {
		for i := range prog.Functions {
			if prog.Functions[i].Name == name {
				return &prog.Functions[i]
			}
		}
		return nil
	}

	// 2. Trace the call graph
	for len(worklist) > 0 {
		funcName := worklist[0]
		worklist = worklist[1:]

		fn := getFunc(funcName)
		if fn == nil || fn.Graph == nil {
			continue // Might be an extern function with no body
		}

		// Look for CallInsts in all blocks
		for _, block := range fn.Graph.Blocks {
			for _, inst := range block.Statements {
				if call, ok := inst.(*mir.CallInst); ok {
					// The function being called is stored in the Function Value
					if reg, isReg := call.Function.(*mir.Register); isReg {
						targetName := reg.Name
						if !liveSet[targetName] {
							liveSet[targetName] = true
							worklist = append(worklist, targetName)
						}
					}
				}
			}
		}
	}

	// 3. Prune the dead functions
	var liveFunctions []mir.Function
	for _, fn := range prog.Functions {
		if liveSet[fn.Name] {
			liveFunctions = append(liveFunctions, fn)
		}
	}
	prog.Functions = liveFunctions
}
