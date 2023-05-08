package pa

import (
	"fmt"
	"go/token"
	"go/types"
	"io"
	"os"
	"runtime/debug"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
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
	config      *Config                      // the client's control/observer interface
	prog        *ssa.Program                 // the program being analyzed
	log         io.Writer                    // log stream; nil to disable
	panicNode   nodeid                       // sink for panic, source for recover
	nodes       []*node                      // indexed by nodeid
	flattenMemo map[types.Type][]*subEleInfo // memoization of flatten()
	cgnodes     []*funcnode                  // all cgnodes
	globalval   map[ssa.Value]nodeid         // node for each global ssa.Value
	globalobj   map[ssa.Value]nodeid         // maps v to sole member of pts(v), if singleton
	localval    map[ssa.Value]nodeid         // node for each local ssa.Value
	localobj    map[ssa.Value]nodeid         // maps v to sole member of pts(v), if singleton
	atFuncs     map[*ssa.Function]bool       // address-taken functions (for presolver)
	mapValues   []nodeid                     // values of makemap objects (indirect in HVN)
	work        nodeset                      // solver's worklist
	result      *Result                      // results of the analysis
	deltaSpace  []int                        // working space for iterating over PTS deltas

	// Reflection & intrinsics:
	hasher              typeutil.Hasher // cache of type hashes
	reflectValueObj     types.Object    // type symbol for reflect.Value (if present)
	reflectValueCall    *ssa.Function   // (reflect.Value).Call
	reflectRtypeObj     types.Object    // *types.TypeName for reflect.rtype (if present)
	reflectRtypePtr     *types.Pointer  // *reflect.rtype
	reflectType         *types.Named    // reflect.Type
	rtypes              typeutil.Map    // nodeid of canonical *rtype-tagged object for type T
	reflectZeros        typeutil.Map    // nodeid of canonical T-tagged object for zero value
	runtimeSetFinalizer *ssa.Function   // runtime.SetFinalizer
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

// Analyze runs the pointer analysis with the scope and options
// specified by config, and returns the (synthetic) root of the callgraph.
//
// Pointer analysis of a transitively closed well-typed program should
// always succeed.  An error can occur only due to an internal bug.
func Analyze(config *Config) (result *Result, err error) {
	if config.Mains == nil {
		return nil, fmt.Errorf("no main/test packages to analyze (check $GOROOT/$GOPATH)")
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("internal error in pointer analysis: %v (please report this bug)", p)
			fmt.Fprintln(os.Stderr, "Internal panic in pointer analysis:")
			debug.PrintStack()
		}
	}()

	a := &analysis{
		config:      config,
		log:         config.Log,
		prog:        config.prog(),
		globalval:   make(map[ssa.Value]nodeid),
		globalobj:   make(map[ssa.Value]nodeid),
		flattenMemo: make(map[types.Type][]*fieldInfo),
		trackTypes:  make(map[types.Type]bool),
		atFuncs:     make(map[*ssa.Function]bool),
		hasher:      typeutil.MakeHasher(),
		intrinsics:  make(map[*ssa.Function]intrinsic),
		result: &Result{
			Queries:         make(map[ssa.Value]Pointer),
			IndirectQueries: make(map[ssa.Value]Pointer),
		},
		deltaSpace: make([]int, 0, 100),
	}

	if false {
		a.log = os.Stderr // for debugging crashes; extremely verbose
	}

	if a.log != nil {
		fmt.Fprintln(a.log, "==== Starting analysis")
	}

	a.solve()

	return a.result, nil
}

func (a *analysis) warningLog(pos token.Pos, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if a.log != nil {
		fmt.Fprintf(a.log, "%s: warning: %s\n", a.prog.Fset.Position(pos), msg)
	}
}

func (a *analysis) normalLog(msg string) {
	if a.log != nil {
		fmt.Fprintf(a.log, msg)
	}
}
