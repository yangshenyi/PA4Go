package pa

import (
	"golang.org/x/tools/go/ssa"
)

// k-CFA context sensitivity.
// this const value define how deep a context could take.
// at least 1
const level int = 1

// shouldUseContext defines the context-sensitivity policy.  It
// returns true if we should analyse all static calls to fn anew.
//
// Obviously this interface rather limits how much freedom we have to
// choose a policy.  The current policy, rather arbitrarily, is true
// for intrinsics and accessor methods (actually: short, single-block,
// call-free functions).  This is just a starting point.
// define whether a function should be treated with context sensitivity
func selectiveContextPolicy(fn *ssa.Function) bool {
	/*
		if len(fn.Blocks) != 1 {
			return false // too expensive
		}
		blk := fn.Blocks[0]
		if len(blk.Instrs) > 10 {
			return false // too expensive
		}
		if fn.Synthetic != "" && (fn.Pkg == nil || fn != fn.Pkg.Func("init")) {
			return true // treat synthetic wrappers context-sensitively
		}
		for _, instr := range blk.Instrs {
			switch instr := instr.(type) {
			case ssa.CallInstruction:
				// Disallow function calls (except to built-ins)
				// because of the danger of unbounded recursion.
				if _, ok := instr.Common().Value.(*ssa.Builtin); !ok {
					return false
				}
			}
		}*/
	return true
}

type context struct {
	callstring [level]ssa.CallInstruction
}

func NewContext() context {
	return context{[level]ssa.CallInstruction{}}
}

func (caller_context context) GenContext(l ssa.CallInstruction) context {
	var new_context [level]ssa.CallInstruction
	for i := 1; i < level; i++ {
		new_context[i-1] = caller_context.callstring[i]
	}
	new_context[level-1] = l
	return context{new_context}
}

// denotes a reachable func with context
type funcnode struct {
	fn           *ssa.Function // func ir info
	obj          nodeid        // start of this function object block
	func_context context
}

// wrapper. duplicate edges  due to the elimination of context
func (a *analysis) addCallGraphEdge(caller *ssa.Function, callsite ssa.CallInstruction, callee *ssa.Function) {
	if _, ok := a.callgraph[caller]; !ok {
		a.callgraph[caller] = make(map[ssa.CallInstruction]map[*ssa.Function]bool)
	}
	if _, ok := a.callgraph[caller][callsite]; !ok {
		a.callgraph[caller][callsite] = make(map[*ssa.Function]bool)
	}
	a.callgraph[caller][callsite][callee] = true
}
