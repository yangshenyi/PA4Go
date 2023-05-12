package pa

import (
	"fmt"
	"go/types"
	"io"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
)

type analysis struct {
	prog            *ssa.Program    // the program being analyzed
	entryfuns       []*ssa.Function // entry points, including main function and exported functions
	log             io.Writer       // log stream; nil to disable
	panicNode       nodeid
	nodes           []*node // indexed by nodeid
	flattenBuf      map[types.Type][]*subEleInfo
	globalval       map[ssa.Value]nodeid // node for each global ssa.Value
	globalobj       map[ssa.Value]nodeid
	csfuncobj       map[ssa.Value]map[context]nodeid
	localval        map[ssa.Value]nodeid // node for each local ssa.Value
	localobj        map[ssa.Value]nodeid
	worklist        nodeset // solver's worklist
	reachable_queue []*funcnode
	deltaSpace      []int

	// result
	callgraph map[*ssa.Function]map[ssa.CallInstruction]map[*ssa.Function]bool // a temp callgraph to efficiently reduce possible redundant edges
	CallGraph *callgraph.Graph                                                 // discovered call graph
}

func Analyze(prog_ *ssa.Program, log_ io.Writer, paks []*ssa.Package, entry_funcs []*ssa.Function) (result *callgraph.Graph, err error) {

	a := &analysis{
		log:        log_,
		entryfuns:  entry_funcs,
		prog:       prog_,
		globalval:  make(map[ssa.Value]nodeid),
		globalobj:  make(map[ssa.Value]nodeid),
		flattenBuf: make(map[types.Type][]*subEleInfo),
		csfuncobj:  make(map[ssa.Value]map[context]nodeid),
		deltaSpace: make([]int, 0, 100),
		nodes:      make([]*node, 0),
	}

	// Pass ssa.package is also ok.
	// the entry functions would be extracted out.
	if paks != nil {
		a.entryfuns = append(a.entryfuns, a.entryPoints(paks)...)
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

	// contruct final call graph
	for f1, call := range a.callgraph {
		for callinstr, f2set := range call {
			for f2 := range f2set {
				callgraph.AddEdge(a.CallGraph.CreateNode(f1), callinstr, a.CallGraph.CreateNode(f2))
			}
		}
	}

	return a.CallGraph, nil
}

func (a *analysis) entryPoints(topPackages []*ssa.Package) []*ssa.Function {
	var entries []*ssa.Function
	for _, pkg := range topPackages {
		if pkg.Pkg.Name() == "main" {
			entries = append(entries, a.memberFuncs(pkg.Members["main"], pkg.Prog)...)
			for name, member := range pkg.Members {
				if strings.HasPrefix(name, "init#") || name == "init" {
					entries = append(entries, a.memberFuncs(member, pkg.Prog)...)
				}
			}
			continue
		}
		for _, member := range pkg.Members {
			for _, f := range a.memberFuncs(member, pkg.Prog) {
				if a.isEntry(f) {
					entries = append(entries, f)
				}
			}
		}
	}
	return entries
}

func (a *analysis) isEntry(f *ssa.Function) bool {
	if f.Name() == "init" && f.Synthetic == "package initializer" {
		return true
	}
	return f.Synthetic == "" && f.Object() != nil && f.Object().Exported()
}

func (a *analysis) memberFuncs(member ssa.Member, prog *ssa.Program) []*ssa.Function {
	switch t := member.(type) {
	case *ssa.Type:
		methods := typeutil.IntuitiveMethodSet(t.Type(), &prog.MethodSets)
		var funcs []*ssa.Function
		for _, m := range methods {
			if f := prog.MethodValue(m); f != nil {
				funcs = append(funcs, f)
			}
		}
		return funcs
	case *ssa.Function:
		return []*ssa.Function{t}
	default:
		return nil
	}
}
