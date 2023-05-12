package main

import (
	"fmt"
	"os"
	"sort"

	pa "github.com/yangshenyi/PA4Go"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func main() {
	const myprog = `
	package main

	
	func void_ (a func(int)){ a(5)}
	func test_fp(func1 func(int)int){
		
		is_ok := func(x int)bool{
			return true
		}

		void_( func(x int){
			if is_ok(x) {
				return 
			}		
			_ = func1(5)
		})
		
		
	}
	
	
	func main() {
	
		test_fp(func(x int)int{return x+1})	
	}
`

	/*
	   inc

	   inc$1
	   # Free variables:
	   #   0:	func2 *func(int) --> t2 --> fn inc$1
	   #   1:	func1 *func(int) int --> func1 --> main$1

	   t1 <== t2		==>		t1 --> inc$1
	   t4 <== func1		==>		t4 --> main$1

	*/
	var conf loader.Config

	// Parse the input file, a string.
	// (Command-line tools should use conf.FromArgs.)
	file, err := conf.ParseFile("myprog.go", myprog)
	if err != nil {
		fmt.Print(err) // parse error
		return
	}

	// Create single-file main package and import its dependencies.
	conf.CreateFromFiles("main", file)

	iprog, err := conf.Load()
	if err != nil {
		fmt.Print(err) // type error in some package
		return
	}

	// Create SSA-form program representation.
	prog := ssautil.CreateProgram(iprog, ssa.InstantiateGenerics)
	mainPkg := prog.Package(iprog.Created[0].Pkg)

	// Build SSA code for bodies of all functions in the whole program.
	prog.Build()

	// print ssa
	mainPkg.WriteTo(os.Stdout)
	for _, mem := range mainPkg.Members {
		if fun, ok := mem.(*ssa.Function); ok {
			fun.WriteTo(os.Stdout)
			if fun.Name() == "test_fp" {
				fmt.Println(1)
				for _, block := range fun.Blocks {
					for _, instr := range block.Instrs {
						if v, ok := instr.(*ssa.MakeClosure); ok {
							v.Fn.(*ssa.Function).WriteTo(os.Stdout)
							fmt.Println(v.Bindings)
						}
					}
				}
			}
		}
	}

	result, err := pa.Analyze(prog, os.Stdout, []*ssa.Package{mainPkg})
	if err != nil {
		panic(err) // internal error in pointer analysis
	}

	var edges []string
	callgraph.GraphVisitEdges(result, func(edge *callgraph.Edge) error {
		caller := edge.Caller.Func
		if caller.Pkg == mainPkg {
			edges = append(edges, fmt.Sprint(caller, " --> ", edge.Callee.Func, " line: ", prog.Fset.Position(edge.Pos()).Line))
		}
		return nil
	})

	// Print the edges in sorted order.
	sort.Strings(edges)
	for _, edge := range edges {
		fmt.Println(edge)
	}
	fmt.Println()

}
