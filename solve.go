package pa

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

// if we find a *ssa.Function called, check if
func (a *analysis) addReachable() {

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
			callgraph.AddEdge(a.CallGraph.CreateNode(root_func), nil, a.CallGraph.CreateNode(fn))
			if a.log != nil {
				fmt.Fprintf(a.log, "\tCallGraph: %s --> %s:\n", a.CallGraph.CreateNode(root_func), a.CallGraph.CreateNode(fn))
			}
		}
	}

}
