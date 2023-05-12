package main

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
