package pa

import (
	"fmt"
	"go/types"
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

// d = s.(typ)  where typ is a concrete type
// A complex Rule attached to src (the interface).
//
// If exact, only tagged objects identical to typ are untagged.
// If !exact, tagged objects assignable to typ are untagged too.
// The latter is needed for various reflect operators, e.g. Send.
//
// This entails a representation change:
// pts(src) contains tagged objects,
// pts(dst) contains their payloads.
type untagRule struct {
	typ   types.Type // a concrete type
	d     nodeid
	exact bool
}

// src.method(params...)
// A complex Rule attached to iface.
type invokeRule struct {
	method *types.Func // the abstract method
	iface  nodeid      // (ptr) the interface
	params nodeid      // the start of the identity/params/results block
}

func (c *loadRule) addflow(a *analysis, delta *nodeset) {
	var changed bool
	for _, x := range delta.AppendTo(a.deltaSpace) {
		k := nodeid(x)
		koff := k + nodeid(c.offset)
		if a.onlineCopy(c.dst, koff) {
			changed = true
		}
	}
	if changed {
		a.addWork(c.dst)
	}
}

func (c *storeRule) addflow(a *analysis, delta *nodeset) {
	for _, x := range delta.AppendTo(a.deltaSpace) {
		k := nodeid(x)
		koff := k + nodeid(c.offset)
		if a.onlineCopy(koff, c.src) {
			a.addWork(koff)
		}
	}
}

func (c *offsetAddrRule) addflow(a *analysis, delta *nodeset) {
	dst := a.nodes[c.dst]
	for _, x := range delta.AppendTo(a.deltaSpace) {
		k := nodeid(x)
		if dst.addflow.pts.add(k + nodeid(c.offset)) {
			a.addWork(c.dst)
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
			if a.addLabel(c.dst, ifaceObj) {
				a.addWork(c.dst)
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
		tDyn, v, indirect := a.taggedValue(ifaceObj)
		if indirect {
			// TODO(adonovan): we'll need to implement this
			// when we start creating indirect tagged objects.
			panic("indirect tagged object")
		}

		if predicate(tDyn, c.typ) {
			// Copy payload sans tag to dst.
			//
			// TODO(adonovan): opt: if tDyn is
			// nonpointerlike we can skip this entire
			// Rule, perhaps.  We only care about
			// pointers among the fields.
			a.onlineCopyN(c.dst, v, a.sizeof(tDyn))
		}
	}
}

func (c *invokeRule) addflow(a *analysis, delta *nodeset) {
	for _, x := range delta.AppendTo(a.deltaSpace) {
		ifaceObj := nodeid(x)
		tDyn, v, indirect := a.taggedValue(ifaceObj)
		if indirect {
			// TODO(adonovan): we may need to implement this if
			// we ever apply invokeRules to reflect.Value PTSs,
			// e.g. for (reflect.Value).Call.
			panic("indirect tagged object")
		}

		// Look up the concrete method.
		fn := a.prog.LookupMethod(tDyn, c.method.Pkg(), c.method.Name())
		if fn == nil {
			panic(fmt.Sprintf("n%d: no ssa.Function for %s", c.iface, c.method))
		}
		sig := fn.Signature

		fnObj := a.globalobj[fn] // dynamic calls use shared contour
		if fnObj == 0 {
			// a.objectNode(fn) was not called during gen phase.
			panic(fmt.Sprintf("a.globalobj[%s]==nil", fn))
		}

		// Make callsite's fn variable point to identity of
		// concrete method.  (There's no need to add it to
		// worklist since it never has attached Rules.)
		a.addLabel(c.params, fnObj)

		// Extract value and connect to method's receiver.
		// Copy payload to method's receiver param (arg0).
		arg0 := a.funcParams(fnObj)
		recvSize := a.sizeof(sig.Recv().Type())
		a.onlineCopyN(arg0, v, recvSize)

		src := c.params + 1 // skip past identity
		dst := arg0 + nodeid(recvSize)

		// Copy caller's argument block to method formal parameters.
		paramsSize := a.sizeof(sig.Params())
		a.onlineCopyN(dst, src, paramsSize)
		src += nodeid(paramsSize)
		dst += nodeid(paramsSize)

		// Copy method results to caller's result block.
		resultsSize := a.sizeof(sig.Results())
		a.onlineCopyN(src, dst, resultsSize)
	}
}
