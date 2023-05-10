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

import "fmt"

type I interface {
	f(map[string]int)
}

type C struct{}

func inc(func1 func(int)int){
	var func2 func(int)
	func2 = func(x int){
		if x==0 {
			return
		}
		func2(x-1)
		_ = func1(5)            
	}

	func2(5)
}

func (C) f(m map[string]int) {
	fmt.Println("C.f()")
}

type B struct{}
func (B) f(m map[string]int) {
	fmt.Println("B.f()")
}


func main() {
	var i I = C{}
	x := map[string]int{"one":1}
	i.f(x) // dynamic method call
	k:=5
	inc(func(x int)int{return x+1})
	_ = k
}

`
	/*

	   inc

	   inc$1
	   # Free variables:
	   #   0:	func2 *func(int) --> t2 --> fn inc$1
	   #   1:	func1 *func(int) int --> func1 --> main$1

	   t1 <== t1	==>	t1->inc$1
	   t4 <== func1	==>	t4->main$1

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
			if fun.Name() == "inc" {
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

	result, err := pa.Analyze(prog, nil, []*ssa.Package{mainPkg})
	if err != nil {
		panic(err) // internal error in pointer analysis
	}

	var edges []string
	callgraph.GraphVisitEdges(result, func(edge *callgraph.Edge) error {
		caller := edge.Caller.Func
		if caller.Pkg == mainPkg {
			edges = append(edges, fmt.Sprint(caller, " --> ", edge.Callee.Func))
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
