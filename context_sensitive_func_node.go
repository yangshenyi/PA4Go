package pa

import (
	"golang.org/x/tools/go/ssa"
)

// k-CFA context sensitivity.
// this const value define how deep a context could take.
// at least 1
const level int = 1

// define whether a function should be treated with context sensitivity
func selectiveContextPolicy(callee *ssa.Function) bool {
	return true
}

type context struct {
	callstring [level]*ssa.CallInstruction
}

func (*context) NewContext() context {
	return context{[level]*ssa.CallInstruction{}}
}

func (*context) GenContext(caller_context context, l *ssa.CallInstruction) context {
	var new_context [level]*ssa.CallInstruction
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
