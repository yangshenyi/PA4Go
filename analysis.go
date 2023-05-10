package pa

import (
	"fmt"
	"go/types"
	"io"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

// An analysis instance holds the state of a single pointer analysis problem.
type analysis struct {
	prog            *ssa.Program // the program being analyzed
	mains           []*ssa.Package
	log             io.Writer                    // log stream; nil to disable
	panicNode       nodeid                       // sink for panic, source for recover
	nodes           []*node                      // indexed by nodeid
	flattenMemo     map[types.Type][]*subEleInfo // memoization of flatten()
	globalval       map[ssa.Value]nodeid         // node for each global ssa.Value
	globalobj       map[ssa.Value]nodeid         // maps v to sole member of pts(v), if singleton
	csfuncobj       map[ssa.Value]map[context]nodeid
	localval        map[ssa.Value]nodeid // node for each local ssa.Value
	localobj        map[ssa.Value]nodeid // maps v to sole member of pts(v), if singleton
	worklist        nodeset              // solver's worklist
	reachable_queue []*funcnode
	deltaSpace      []int // working space for iterating over PTS deltas

	// result
	callgraph map[*ssa.Function]map[ssa.CallInstruction]map[*ssa.Function]bool
	CallGraph *callgraph.Graph // discovered call graph
}

func Analyze(prog_ *ssa.Program, log_ io.Writer, mains_ []*ssa.Package) (result *callgraph.Graph, err error) {

	a := &analysis{
		log:         log_,
		mains:       mains_,
		prog:        prog_,
		globalval:   make(map[ssa.Value]nodeid),
		globalobj:   make(map[ssa.Value]nodeid),
		flattenMemo: make(map[types.Type][]*subEleInfo),
		csfuncobj:   make(map[ssa.Value]map[context]nodeid),
		deltaSpace:  make([]int, 0, 100),
		nodes:       make([]*node, 0),
	}

	if reflect := a.prog.ImportedPackage("reflect"); reflect != nil {
		if a.log != nil {
			fmt.Fprintln(a.log, "reflect not support")
		}
	}
	if runtime := a.prog.ImportedPackage("runtime"); runtime != nil {
		if a.log != nil {
			fmt.Fprintln(a.log, "runtime not support")
		}
	}

	if a.log != nil {
		fmt.Fprintln(a.log, "----------- Starting analysis -----------")
	}

	a.solve()

	for f1, call := range a.callgraph {
		for callinstr, f2set := range call {
			for f2 := range f2set {
				callgraph.AddEdge(a.CallGraph.CreateNode(f1), callinstr, a.CallGraph.CreateNode(f2))
			}
		}
	}

	return a.CallGraph, nil
}
