package pa

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

type rule interface {
	addflow(a *analysis, delta *nodeset)
}

// d = s[offset]
type loadRule struct {
	offset uint32
	d      nodeid
}

// d[offset] = s
type storeRule struct {
	offset uint32
	s      nodeid
}

// d = &s.[offset]
type offsetAddrRule struct {
	offset uint32
	d      nodeid
}

// d = s.(typ)  where typ is an interface
type typeFilterRule struct {
	typ types.Type // an interface type
	d   nodeid
}

type untagRule struct {
	typ   types.Type // a concrete type
	d     nodeid
	exact bool
}

// src.method(params...)
// A complex Rule attached to iface.
type invokeRule struct {
	caller *funcnode
	site   ssa.CallInstruction
	method *types.Func // the abstract method
	params nodeid      // the start of the identity/params/results block
}

// fp
type fpRule struct {
	caller *funcnode
	site   ssa.CallInstruction
	params nodeid // the start of the identity/params/results block
}

// The size of the copy is implicitly 1.
// It returns true if pts(dst) changed.
func (a *analysis) auxaddflow(dst, src nodeid) bool {
	if dst != src {
		if nsrc := a.nodes[src]; nsrc.flow_to.add(dst) {
			if a.log != nil {
				fmt.Fprintf(a.log, "\t\t\tdynamic copy n%d <- n%d\n", dst, src)
			}
			return a.nodes[dst].pts.addAll(&nsrc.pts)
		}
	}
	return false
}

// Returns sizeof.
// Implicitly adds nodes to worklist.
func (a *analysis) auxaddflowN(dst, src nodeid, sizeof uint32) uint32 {
	for i := uint32(0); i < sizeof; i++ {
		if a.auxaddflow(dst, src) {
			a.addWork(dst)
		}
		src++
		dst++
	}
	return sizeof
}

func (c *loadRule) addflow(a *analysis, delta *nodeset) {
	var changed bool
	for _, x := range delta.AppendTo(a.deltaSpace) {
		k := nodeid(x)
		koff := k + nodeid(c.offset)
		if a.auxaddflow(c.d, koff) {
			changed = true
		}
	}
	if changed {
		a.addWork(c.d)
	}
}

func (c *storeRule) addflow(a *analysis, delta *nodeset) {
	for _, x := range delta.AppendTo(a.deltaSpace) {
		k := nodeid(x)
		koff := k + nodeid(c.offset)
		if a.auxaddflow(koff, c.s) {
			a.addWork(koff)
		}
	}
}

func (c *offsetAddrRule) addflow(a *analysis, delta *nodeset) {
	dst := a.nodes[c.d]
	for _, x := range delta.AppendTo(a.deltaSpace) {
		k := nodeid(x)
		if dst.pts.add(k + nodeid(c.offset)) {
			a.addWork(c.d)
		}
	}
}

func (c *typeFilterRule) addflow(a *analysis, delta *nodeset) {
	for _, x := range delta.AppendTo(a.deltaSpace) {
		ifaceObj := nodeid(x)
		tDyn, _, indirect := a.taggedValue(ifaceObj)
		if indirect {
			// TODO(adonovan): we'll need to implement this
			// when we start creating indirect tagged objects.
			panic("indirect tagged object")
		}

		if types.AssignableTo(tDyn, c.typ) {
			if a.nodes[c.d].pts.add(ifaceObj) {
				a.addWork(c.d)
			}
		}
	}
}

func (c *untagRule) addflow(a *analysis, delta *nodeset) {
	predicate := types.AssignableTo
	if c.exact {
		predicate = types.Identical
	}
	for _, x := range delta.AppendTo(a.deltaSpace) {
		ifaceObj := nodeid(x)
		tDyn, v, _ := a.taggedValue(ifaceObj)

		if predicate(tDyn, c.typ) {
			// Copy payload sans tag to dst.
			//
			// TODO(adonovan): opt: if tDyn is
			// nonpointerlike we can skip this entire
			// Rule, perhaps.  We only care about
			// pointers among the fields.
			a.auxaddflowN(c.d, v, a.sizeof(tDyn))
		}
	}
}

func (c *invokeRule) addflow(a *analysis, delta *nodeset) {
	for _, x := range delta.AppendTo(a.deltaSpace) {
		ifaceObj := nodeid(x)
		tDyn, v, _ := a.taggedValue(ifaceObj)

		// Look up the concrete method.
		fn := a.prog.LookupMethod(tDyn, c.method.Pkg(), c.method.Name())
		if fn == nil {
			panic(fmt.Sprintf("no ssa.Function for %s", c.method))
		}

		if pkg := fn.Pkg; pkg != nil {
			if pkg.Pkg.Name() == "reflect" {
				continue
			}
			if pkg.Pkg.Name() == "runtime" {
				continue
			}
		}

		sig := fn.Signature

		var obj nodeid

		// Find related context, if exists.
		// or create a new function object with context generated
		new_context := c.caller.func_context.GenContext(c.site)
		if _, ok := a.csfuncobj[fn]; !ok {
			a.csfuncobj[fn] = make(map[context]nodeid, 0)
		}
		newly_add := false
		if selectiveContextPolicy(fn) {
			if func_obj, ok2 := a.csfuncobj[fn][new_context]; ok2 {
				obj = func_obj
			} else {
				obj = a.makeFunctionObject(fn)
				a.csfuncobj[fn][new_context] = obj
				newly_add = true
			}
		} else {
			if func_obj, ok2 := a.csfuncobj[fn][NewContext()]; ok2 {
				obj = func_obj
			} else {
				obj = a.makeFunctionObject(fn)
				a.csfuncobj[fn][NewContext()] = obj
				newly_add = true
			}
		}

		if newly_add {
			new_funcnode := &funcnode{fn, obj, new_context}

			// Set called function obj's obj field.
			a.nodes[obj].obj.funcn = new_funcnode

			// Add newly added funcnode into reachable queue
			a.addReachable(*new_funcnode)
		}

		a.addCallGraphEdge(c.caller.fn, c.site, fn)

		// Extract value and connect to method's receiver.
		// Copy payload to method's receiver param (arg0).
		arg0 := a.funcParams(obj)
		recvSize := a.sizeof(sig.Recv().Type())
		a.auxaddflowN(arg0, v, recvSize)

		src := c.params
		dst := arg0 + nodeid(recvSize)

		// Copy caller's argument block to method formal parameters.
		paramsSize := a.sizeof(sig.Params())
		a.auxaddflowN(dst, src, paramsSize)
		src += nodeid(paramsSize)
		dst += nodeid(paramsSize)

		// Copy method results to caller's result block.
		resultsSize := a.sizeof(sig.Results())
		a.auxaddflowN(src, dst, resultsSize)
	}
}

func (c *fpRule) addflow(a *analysis, delta *nodeset) {

	for _, x := range delta.AppendTo(a.deltaSpace) {
		funcobj := nodeid(x)

		// Look up the concrete method.
		var fn *ssa.Function
		var ok bool
		if fn, ok = a.nodes[funcobj].obj.data.(*ssa.Function); !ok {
			panic(fmt.Sprintf("no ssa.Function for %s", c.site))
		}

		sig := fn.Signature

		var obj nodeid

		// Find related context, if exists.
		// or create a new function object with context generated
		new_context := c.caller.func_context.GenContext(c.site)
		if _, ok := a.csfuncobj[fn]; !ok {
			a.csfuncobj[fn] = make(map[context]nodeid, 0)
		}
		newly_add := false
		if selectiveContextPolicy(fn) {
			if func_obj, ok2 := a.csfuncobj[fn][new_context]; ok2 {
				obj = func_obj
			} else {
				obj = a.makeFunctionObject(fn)
				a.csfuncobj[fn][new_context] = obj
				newly_add = true
			}
		} else {
			if func_obj, ok2 := a.csfuncobj[fn][NewContext()]; ok2 {
				obj = func_obj
			} else {
				obj = a.makeFunctionObject(fn)
				a.csfuncobj[fn][NewContext()] = obj
				newly_add = true
			}
		}

		if newly_add {
			new_funcnode := &funcnode{fn, obj, new_context}

			// Set called function obj's obj field.
			a.nodes[obj].obj.funcn = new_funcnode

			// Add newly added funcnode into reachable queue
			a.addReachable(*new_funcnode)
		}
		//fmt.Println(newly_add, fn.Signature, fn.Name(), fn.FreeVars)

		a.addCallGraphEdge(c.caller.fn, c.site, fn)

		/*
			// flush freevars
			for _, fre := range fn.FreeVars {
				a.nodes[a.valueNode(fre)].prev_pts.Clear()
				a.worklist.add(a.valueNode(fre))
			}*/

		src := c.params
		dst := a.funcParams(obj)

		// Copy caller's argument block to method formal parameters.
		paramsSize := a.sizeof(sig.Params())
		a.auxaddflowN(dst, src, paramsSize)
		src += nodeid(paramsSize)
		dst += nodeid(paramsSize)

		// Copy method results to caller's result block.
		resultsSize := a.sizeof(sig.Results())
		a.auxaddflowN(src, dst, resultsSize)
	}
}
