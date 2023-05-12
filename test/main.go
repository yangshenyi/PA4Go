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

	/*
	   闭包 绑定函数
	   res --> fn obj
	   freevar -->binding

	   pass
	   res ==> param
	   take res node free var node 之后才能拿到 param node
	   才能 add reachable，从 free var 拿对象
	*/
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
	file, err := conf.ParseFile("myprog.go", myprog_interface_invoke)
	if err != nil {
		fmt.Print(err) // parse error
		return
	}

	conf.CreateFromFiles("mytest", file)
	iprog, err := conf.Load()
	if err != nil {
		fmt.Print(err)
		return
	}

	prog := ssautil.CreateProgram(iprog, ssa.InstantiateGenerics)
	mainPkg := prog.Package(iprog.Created[0].Pkg)

	prog.Build()
	fmt.Println(mainPkg.Members)
	mainPkg.WriteTo(os.Stdout)
	for _, mem := range mainPkg.Members {
		if fun, ok := mem.(*ssa.Function); ok {
			fun.WriteTo(os.Stdout)
			if fun.Name() == "test_fp" {
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

	result, err := pa.Analyze(prog, nil, []*ssa.Package{mainPkg}, nil)
	if err != nil {
		panic(err)
	}

	var edges []string
	callgraph.GraphVisitEdges(result, func(edge *callgraph.Edge) error {
		caller := edge.Caller.Func
		if caller.Pkg == mainPkg {
			edges = append(edges, fmt.Sprint(caller, " --> ", edge.Callee.Func, " line: ", prog.Fset.Position(edge.Pos()).Line))
		}
		return nil
	})

	sort.Strings(edges)
	for _, edge := range edges {
		fmt.Println(edge)
	}
	fmt.Println()

}
