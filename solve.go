package pa

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

// duplication check is done before.
// that is to say, a fc passed here should not be analyzed before.
func (a *analysis) addReachable(fc funcnode) {
	// queue for deterministic func call
	a.reachable_queue = make([]*funcnode, 0)
	a.reachable_queue = append(a.reachable_queue, &fc)

	for len(a.reachable_queue) > 0 {
		cfc := a.reachable_queue[0]
		a.reachable_queue = a.reachable_queue[1:]
		a.genFunc(cfc)
	}
}

func (a *analysis) propagate() {

}

// ------------- deal with function call ------------------
// with selective context sisitivity applied, see whether there exists a relavant funcnode

func (a *analysis) solveStaticCall() {

}

func (a *analysis) solveInterfaceDynamicCall() {

}

func (a *analysis) solveFpDynamicCall() {

}

func (a *analysis) solve() {
	// ------------ init -----------------

	// Create a dummy node for non-pointerlike variables.
	a.addNodes(tInvalid, "(zero)")

	// Create the global node for panic values.
	a.panicNode = a.addNodes(tEface, "panic")

	// generate synthesis root node
	root_func := a.prog.NewFunction("<synthesis root>", new(types.Signature), "root")
	a.CallGraph = callgraph.New(root_func)
	a.callgraph[root_func] = make(map[ssa.CallInstruction]map[*ssa.Function]bool)
	// addreachable for entry point func
	// For each main package, call main.init(), main.main().
	for _, mainPkg := range a.mains {
		main := mainPkg.Func("main")
		if main == nil {
			panic(fmt.Sprintf("%s has no main function", mainPkg))
		}

		for _, fn := range [2]*ssa.Function{mainPkg.Func("init"), main} {
			if a.log != nil {
				fmt.Fprintf(a.log, "\troot call to %s:\n", fn)
			}
			a.addCallGraphEdge(root_func, nil, fn)
			if a.log != nil {
				fmt.Fprintf(a.log, "\tCallGraph: %s --> %s:\n", root_func.Name(), fn.Name())
			}
			new_func_obj_id := a.makeFunctionObject(fn)
			new_context := NewContext()
			if _, ok := a.csfuncobj[fn]; !ok {
				a.csfuncobj[fn] = make(map[context]nodeid)
			}
			a.csfuncobj[fn][new_context] = new_func_obj_id
			a.addReachable(funcnode{fn, new_func_obj_id, new_context})
		}
	}

}

func (a *analysis) addWork(id nodeid) {
	a.worklist.Insert(int(id))
	if a.log != nil {
		fmt.Fprintf(a.log, "\t\twork: n%d\n", id)
	}
}
