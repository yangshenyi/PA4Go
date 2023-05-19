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

	for _, x := range a.globalflushbuf.AppendTo(a.flushSpace) {
		a.nodes[nodeid(x)].prev_pts.Clear()
		a.worklist.add(nodeid(x))
	}
	a.globalflushbuf.Clear()
	a.flushSpace = a.flushSpace[:0]

}

// one level spread
func (a *analysis) propagate(n *node, delta *nodeset) {
	n.prev_pts.Copy(&n.pts.Sparse)

	var copySeen nodeset
	for _, x := range n.flow_to.AppendTo(a.deltaSpace) {
		mid := nodeid(x)
		if copySeen.add(mid) {
			if a.nodes[mid].pts.addAll(delta) {
				a.addWork(mid)
			}
		} else {
			fmt.Println("????? duplication???s")
		}
	}
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
	a.callgraph = make(map[*ssa.Function]map[ssa.CallInstruction]map[*ssa.Function]bool)
	a.callgraph[root_func] = make(map[ssa.CallInstruction]map[*ssa.Function]bool)

	for _, entry := range a.entryfuns {

		if a.log != nil {
			fmt.Fprintf(a.log, "\troot call to %s:\n", entry)
		}
		a.addCallGraphEdge(root_func, nil, entry)
		if a.log != nil {
			fmt.Fprintf(a.log, "\tCallGraph: %s --> %s:\n", root_func.Name(), entry.Name())
		}
		new_func_obj_id := a.makeFunctionObject(entry)
		new_context := NewContext()
		if _, ok := a.csfuncobj[entry]; !ok {
			a.csfuncobj[entry] = make(map[context]nodeid)
		}
		a.csfuncobj[entry][new_context] = new_func_obj_id
		a.addReachable(funcnode{entry, new_func_obj_id, new_context})

	}

	// ------------ worklist iteration -----------------

	if a.log != nil {
		fmt.Fprintf(a.log, "\n\n----- Solving through worklist ---------\n\n")
	}

	var delta nodeset
	for {
		var x int
		if !a.worklist.TakeMin(&x) {
			break // empty
		}
		id := nodeid(x)
		if a.log != nil {
			fmt.Fprintf(a.log, "\ttake node n%d\n", id)
		}

		n := a.nodes[id]

		// Difference propagation.
		delta.Difference(&n.pts.Sparse, &n.prev_pts.Sparse)
		if delta.IsEmpty() {
			continue
		}
		a.propagate(n, &delta)

		if a.log != nil {
			fmt.Fprintf(a.log, "\t\tpts(n%d : %s) = %s + %s\n",
				id, n.typ, &delta, &n.pts)
		}

		// Apply all resolution rules attached to n.
		for _, rule := range n.fly_solve {
			if a.log != nil {
				fmt.Fprintf(a.log, "\t\trule %s\n", rule)
			}
			rule.addflow(a, &delta)
		}

	}

	if !a.nodes[0].pts.IsEmpty() {
		panic(fmt.Sprintf("pts(0) is nonempty: %s", &a.nodes[0].pts))
	}

	// Release buffer except for final pts
	for _, n := range a.nodes {
		n.fly_solve = nil
		n.flow_to.Clear()
		n.prev_pts.Clear()
	}

	if a.log != nil {
		fmt.Fprintf(a.log, "Solver done\n")

		// Dump solution.
		for i, n := range a.nodes {
			if !n.pts.IsEmpty() {
				fmt.Fprintf(a.log, "pts(n%d) = %s : %s\n", i, &n.pts, n.typ)
			}
		}
	}

}

func (a *analysis) addWork(id nodeid) {
	a.worklist.Insert(int(id))
	if a.log != nil {
		fmt.Fprintf(a.log, "\t\twork: n%d\n", id)
	}
}
