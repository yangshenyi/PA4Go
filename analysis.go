package pa

import (
	"fmt"
	"go/token"
	"go/types"
	"io"
	"os"
	"runtime/debug"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

// A Config formulates a pointer analysis problem for Analyze. It is
// only usable for a single invocation of Analyze and must not be
// reused.
type Config struct {
	Mains []*ssa.Package

	Log io.Writer
}

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

// enclosingObj returns the first node of the addressable memory
// object that encloses node id.  Panic ensues if that node does not
// belong to any object.
func (a *analysis) enclosingObj(id nodeid) nodeid {
	// Find previous node with obj != nil.
	for i := id; i >= 0; i-- {
		n := a.nodes[i]
		if obj := n.obj; obj != nil {
			if i+nodeid(obj.size) <= id {
				break // out of bounds
			}
			return i
		}
	}
	panic("node has no enclosing object")
}

func Analyze(prog_ *ssa.Program, log_ io.Writer, mains_ []*ssa.Package) (result *callgraph.Graph, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("internal error in pointer analysis: %v (please report this bug)", p)
			fmt.Fprintln(os.Stderr, "Internal panic in pointer analysis:")
			debug.PrintStack()
		}
	}()

	a := &analysis{
		log:         log_,
		mains:       mains_,
		prog:        prog_,
		globalval:   make(map[ssa.Value]nodeid),
		globalobj:   make(map[ssa.Value]nodeid),
		flattenMemo: make(map[types.Type][]*subEleInfo),
		csfuncobj:   make(map[ssa.Value]map[context]nodeid),
		deltaSpace:  make([]int, 0, 100),
	}

	if false {
		a.log = os.Stderr // for debugging crashes; extremely verbose
	}

	if a.log != nil {
		fmt.Fprintln(a.log, "==== Starting analysis")
	}

	a.solve()

	return a.CallGraph, nil
}

func (a *analysis) warningLog(pos token.Pos, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if a.log != nil {
		fmt.Fprintf(a.log, "%s: warning: %s\n", a.prog.Fset.Position(pos), msg)
	}
}
