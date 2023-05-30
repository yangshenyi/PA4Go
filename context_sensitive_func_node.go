package pa

import (
	"golang.org/x/tools/go/ssa"
)

// k-CFA context sensitivity.
// this const value define how deep a context could take.
// at least 1
const level int = 1

// selective context-sensitivity policy.
func selectiveContextPolicy(fn *ssa.Function) bool {
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

// wrapper. duplicate edges due to the elimination of context
func (a *analysis) addCallGraphEdge(caller *ssa.Function, callsite ssa.CallInstruction, callee *ssa.Function) {
	if _, ok := a.callgraph[caller]; !ok {
		a.callgraph[caller] = make(map[ssa.CallInstruction]map[*ssa.Function]bool)
	}
	if _, ok := a.callgraph[caller][callsite]; !ok {
		a.callgraph[caller][callsite] = make(map[*ssa.Function]bool)
	}
	a.callgraph[caller][callsite][callee] = true
}
