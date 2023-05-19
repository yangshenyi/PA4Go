package main

import (
	"fmt"
	"os"
	"sort"

	pa "github.com/yangshenyi/PA4Go"
	visual "github.com/yangshenyi/PA4Go/visualize"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func main() {

	/*
		再理解理解
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
	file, err := conf.ParseFile("myprog.go", myprog_context)
	if err != nil {
		fmt.Print(err) // parse error
		return
	}

	conf.CreateFromFiles("main", file)
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
							fmt.Println(v.Bindings, v.Fn.Name(), v.Fn.(*ssa.Function).Pkg.Pkg.Path(), v.Fn.(*ssa.Function).Parent().Pkg.Pkg.Path())
						}
					}
				}
			}
			if fun.Name() == "main" {
				for _, block := range fun.Blocks {
					for _, instr := range block.Instrs {
						if v, ok := instr.(*ssa.Field); ok {
							fmt.Println(v.Field, v.X.Type())
						}

						if v, ok := instr.(*ssa.FieldAddr); ok {
							fmt.Println("addr", v.Field, v.X.Type())
						}
						if v, ok := instr.(*ssa.Index); ok {
							fmt.Println("index", v.X.Type())
						}
						if v, ok := instr.(*ssa.IndexAddr); ok {
							fmt.Println("indexaddr", v.X.Type())
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
	visual.PrintOutput(prog, mainPkg, result, nil, true, false)

}

const myprog_select_function = `
package main

import "fmt"

func Testp(x int){
	fmt.Println("ok")
	clo := func()int{ return x+1 }

	func(){clo()}()
}

type I interface {
	f(map[string]int)
}

type C struct{}
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
	i.f(x) 	// dynamic interface call
}
`

const myprog_interface_invoke = `
package main

import "fmt"

type I interface {
	f(map[string]int)
}

type C struct{}
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
	i.f(x) 	// dynamic interface call
}
`
const myprog_function_pointer = `
package main

import "fmt"

func test_fp(func1 func(int)int){
	var func2 func(int)

	func2 = func(x int){
		if x==0 {
			return 
		}		
		func2(x-1)
		_ = func1(5)
	}
	
	func2(6)
}


func main() {
	
	test_fp(func(x int)int{ fmt.Println(x); return x+1})	

	test_fp(func(x int)int{return x+1})	
}
`

const myprog_closure = `
package main

import  "fmt"

type testglobal struct{
	f func(int)
}
var a testglobal

type iface interface{
	Ha()
}

type ia struct{}
func (a ia)Ha(){fmt.Println(a)}

type ib struct{}
func (b ib)Ha(){fmt.Println(b)}

func static_test_function_value(fp func(int)){ fp(-1) } // call mode
func dynamic_after_static(){ a.f(2) } // call mode & 全局变量

func main() {
	a.f = func(x int){fmt.Println(x)}
	var iv iface = ia{}
	free_var := func(iv iface){ iv.Ha() }	// invoke mode

	static_test_function_value( func(x int){
		free_var(iv)			// 闭包自由变量绑定
		dynamic_after_static() 			
	})	
}
`

const myprog_context = `
package main

import "fmt"

type I interface {
	f(map[string]int)
}

type C struct{}
func (C) f(m map[string]int) {
	fmt.Println("C.f()")
}

type B struct{}
func (B) f(m map[string]int) {
	fmt.Println("B.f()")
}

func test_context(it I)I{
	return it
}

func main() {
	var i I = C{}
	
	x := map[string]int{"one":1}
	test_context(i).f(x)

	i = B{}
	test_context(i).f(x)

}
`

const myprog_context2 = `
package main

import "fmt"

type I interface {
	f(map[string]int)
}

type C struct{}
func (C) f(m map[string]int) {
	fmt.Println("C.f()")
}

type B struct{}
func (B) f(m map[string]int) {
	fmt.Println("B.f()")
}

func test_context(it I)I{
	return it
}

func wrap(it I) I {
	return test_context(it)
}

func main() {
	var i I = C{}
	
	x := map[string]int{"one":1}
	wrap(i).f(x)

	i = B{}
	wrap(i).f(x)

}
`

const myprog_field = `
package main

type C struct{fp func(int)int}

func fc1(x int)int {return x+1}
func fc2(x int)int {return x+2}

func main() {
	c1 := C{fc1}
	c2 := C{fc2}
	
	_ = c1.fp(1)
	_ = c2.fp(2)

}
`
