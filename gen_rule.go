package pa

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/yangshenyi/PA4Go/vendo/typeparams"
	"golang.org/x/tools/go/ssa"
)

// ---------- rule creation ----------

// copy creates a constraint of the form dst = src.
// sizeof is the width (in logical fields) of the copied type.
func (a *analysis) addflow(dst, src nodeid, sizeof uint32, instr ssa.Value) {
	if src == dst || sizeof == 0 {
		return // trivial
	}
	if src == 0 || dst == 0 {
		panic(fmt.Sprintf("ill-typed copy dst=n%d src=n%d", dst, src))
	}
	a.auxaddflowN(dst, src, sizeof)
}

// typeAssert creates a typeFilter or untag constraint of the form dst = src.(T):
// typeFilter for an interface, untag for a concrete type.
// The exact flag is specified as for untagConstraint.
func (a *analysis) typeAssert(T types.Type, dst, src nodeid, exact bool) {
	if isInterface(T) {
		a.nodes[src].fly_solve = append(a.nodes[src].fly_solve, &typeFilterRule{T, dst})
	} else {
		a.nodes[src].fly_solve = append(a.nodes[src].fly_solve, &untagRule{T, dst, exact})
	}
}

// copyElems generates load/store constraints for *dst = *src,
// where src and dst are slices or *arrays.
func (a *analysis) copyElems(cgn *funcnode, typ types.Type, dst, src ssa.Value) {
	tmp := a.addNodes(typ, "copy")
	sz := a.sizeof(typ)
	a.genLoad(tmp, a.valueNode(src), 1, sz)
	a.genStore(a.valueNode(dst), tmp, 1, sz)
}

// genConv generates constraints for the conversion operation conv.
func (a *analysis) genConv(conv *ssa.Convert, cfc *funcnode) {
	res := a.valueNode(conv)
	if res == 0 {
		return // result is non-pointerlike
	}

	tSrc := conv.X.Type()
	tDst := conv.Type()

	switch utSrc := tSrc.Underlying().(type) {
	case *types.Slice:
		// []byte/[]rune -> string?
		return

	case *types.Pointer:
		// *T -> unsafe.Pointer?
		if tDst.Underlying() == tUnsafePtr {
			return // we don't model unsafe aliasing (unsound)
		}

	case *types.Basic:
		switch tDst.Underlying().(type) {
		case *types.Pointer:
			// Treat unsafe.Pointer->*T conversions like
			// new(T) and create an unaliased object.
			if utSrc == tUnsafePtr {
				obj := a.addNodes(mustDeref(tDst), "unsafe.Pointer conversion")
				a.endObject(obj, cfc, conv)
				a.nodes[res].pts.add(obj)
				a.worklist.add(res)
				return
			}

		case *types.Slice:
			// string -> []byte/[]rune (or named aliases)?
			if utSrc.Info()&types.IsString != 0 {
				obj := a.addNodes(sliceToArray(tDst), "convert")
				a.endObject(obj, cfc, conv)
				a.nodes[res].pts.add(obj)
				a.worklist.add(res)
				return
			}

		case *types.Basic:
			// All basic-to-basic type conversions are no-ops.
			// This includes uintptr<->unsafe.Pointer conversions,
			// which we (unsoundly) ignore.
			return
		}
	}

	panic(fmt.Sprintf("illegal *ssa.Convert %s -> %s: %s", tSrc, tDst, conv.Parent()))
}

// genAppend generates constraints for a call to append.
func (a *analysis) genAppend(instr *ssa.Call, cgn *funcnode) {
	// Consider z = append(x, y).   y is optional.
	// This may allocate a new [1]T array; call its object w.
	// We get the following constraints:
	// 	z = x
	// 	z = &w
	//     *z = *y

	x := instr.Call.Args[0]

	z := instr
	a.addflow(a.valueNode(z), a.valueNode(x), 1, instr) // z = x

	if len(instr.Call.Args) == 1 {
		return // no allocation for z = append(x) or _ = append(x).
	}

	y := instr.Call.Args[1]
	tArray := sliceToArray(instr.Call.Args[0].Type())

	w := a.nextNode()
	a.addNodes(tArray, "append")
	a.endObject(w, cgn, instr)

	a.copyElems(cgn, tArray.Elem(), z, y) // *z = *y
	a.nodes[a.valueNode(z)].pts.add(w)
	a.worklist.add(a.valueNode(z))
}

// genBuiltinCall generates contraints for a call to a built-in.
func (a *analysis) genBuiltinCall(instr ssa.CallInstruction, cgn *funcnode) {
	call := instr.Common()
	switch call.Value.(*ssa.Builtin).Name() {
	case "append":
		// Safe cast: append cannot appear in a go or defer statement.
		a.genAppend(instr.(*ssa.Call), cgn)

	case "copy":
		tElem := call.Args[0].Type().Underlying().(*types.Slice).Elem()
		a.copyElems(cgn, tElem, call.Args[0], call.Args[1])

	case "panic":
		a.addflow(a.panicNode, a.valueNode(call.Args[0]), 1, call.Value) // z = x

	case "recover":
		if v := instr.Value(); v != nil {
			a.addflow(a.valueNode(v), a.panicNode, 1, call.Value)
		}

	case "print":
		// In the tests, the probe might be the sole reference
		// to its arg, so make sure we create nodes for it.
		if len(call.Args) > 0 {
			a.valueNode(call.Args[0])
		}

	case "ssa:wrapnilchk":
		a.addflow(a.valueNode(instr.Value()), a.valueNode(call.Args[0]), 1, call.Value)

	default:
		// No-ops: close len cap real imag complex print println delete.
	}
}

// genStaticCall generates constraints for a statically dispatched function call.
func (a *analysis) genStaticCall(caller *funcnode, site ssa.CallInstruction, call *ssa.CallCommon, result nodeid) {
	fn := call.StaticCallee()

	if pkg := fn.Pkg; pkg != nil {
		if pkg.Pkg.Name() == "reflect" {
			return
		}
		if pkg.Pkg.Name() == "runtime" {
			return
		}
	}

	// Called function object
	var obj nodeid

	// Find related context, if exists.
	// or create a new function object with context generated
	new_context := caller.func_context.GenContext(site)
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

		// Add newly added funcnode into reachable queue
		a.reachable_queue = append(a.reachable_queue, new_funcnode)

		// Set called function obj's obj field.
		a.nodes[obj].obj.funcn = new_funcnode
	}

	a.addCallGraphEdge(caller.fn, site, fn)

	sig := call.Signature()

	// Copy receiver, if any.
	params := a.funcParams(obj)
	args := call.Args
	if sig.Recv() != nil {
		sz := a.sizeof(sig.Recv().Type())
		a.addflow(params, a.valueNode(args[0]), sz, call.Value)
		params += nodeid(sz) // add params, cause receiver is the first param, but not args
		args = args[1:]
	}

	// Copy actual parameters into formal params block.
	for i, arg := range args {
		sz := a.sizeof(sig.Params().At(i).Type())
		a.addflow(params, a.valueNode(arg), sz, call.Value)
		params += nodeid(sz)
	}

	// Copy formal results block to actual result.
	if result != 0 {
		a.addflow(result, a.funcResults(obj), a.sizeof(sig.Results()), call.Value)
	}

}

// genDynamicCall generates constraints for a dynamic function call.
func (a *analysis) genDynamicCall(caller *funcnode, site ssa.CallInstruction, call *ssa.CallCommon, result nodeid) {
	// We add dynamic closure rules that store the arguments into
	// the P-block and load the results from the R-block of each
	// function discovered in pts(targets).

	sig := call.Signature()

	// Allocate a contiguous relay params/results block for this call.
	block := a.nextNode()
	p := a.addNodes(sig.Params(), "fp.params")
	r := a.addNodes(sig.Results(), "fp.results")

	// Copy the actual parameters into the call's params block.
	for i, n := 0, sig.Params().Len(); i < n; i++ {
		sz := a.sizeof(sig.Params().At(i).Type())
		a.addflow(p, a.valueNode(call.Args[i]), sz, call.Value)
		p += nodeid(sz)
	}
	// Copy the call's results block to the actual results.
	if result != 0 {
		a.addflow(result, r, a.sizeof(sig.Results()), call.Value)
	}

	// We add a dynamic invoke constraint that will connect the
	// caller's and the callee's P/R blocks for each discovered
	// call target.
	a.nodes[a.valueNode(call.Value)].fly_solve = append(a.nodes[a.valueNode(call.Value)].fly_solve, &fpRule{caller, site, block})
}

// genInvoke generates constraints for a dynamic method invocation.
func (a *analysis) genInvoke(caller *funcnode, site ssa.CallInstruction, call *ssa.CallCommon, result nodeid) {

	sig := call.Signature()

	// Allocate a contiguous relay params/results block for this call.
	block := a.nextNode()
	p := a.addNodes(sig.Params(), "invoke.params")
	r := a.addNodes(sig.Results(), "invoke.results")

	// Copy the actual parameters into the call's params block.
	for i, n := 0, sig.Params().Len(); i < n; i++ {
		sz := a.sizeof(sig.Params().At(i).Type())
		a.addflow(p, a.valueNode(call.Args[i]), sz, call.Value)
		p += nodeid(sz)
	}
	// Copy the call's results block to the actual results.
	if result != 0 {
		a.addflow(result, r, a.sizeof(sig.Results()), call.Value)
	}

	// We add a dynamic invoke constraint that will connect the
	// caller's and the callee's P/R blocks for each discovered
	// call target.
	a.nodes[a.valueNode(call.Value)].fly_solve = append(a.nodes[a.valueNode(call.Value)].fly_solve, &invokeRule{caller, site, call.Method, block})
}

// genCall generates constraints for call instruction instr.
func (a *analysis) genCall(cfc *funcnode, instr ssa.CallInstruction) {
	call := instr.Common()

	// Intrinsic implementations of built-in functions.
	if _, ok := call.Value.(*ssa.Builtin); ok {
		a.genBuiltinCall(instr, cfc)
		return
	}

	var result nodeid
	if v := instr.Value(); v != nil {
		result = a.valueNode(v)
	}

	if call.StaticCallee() != nil {
		a.genStaticCall(cfc, instr, call, result)
	} else if call.IsInvoke() {
		a.genInvoke(cfc, instr, call, result)
	} else {
		a.genDynamicCall(cfc, instr, call, result)
	}

	if a.log != nil {
		fmt.Fprintf(a.log, "\t%s in %s", instr, cfc.fn.Name())
	}
}

// genLoad generates constraints for result = *(ptr + val).
func (a *analysis) genLoad(dst nodeid, ptr nodeid, offset, sizeof uint32) {

	if dst == 0 {
		return // load of non-pointerlike value
	}
	if dst == 0 && ptr == 0 {
		return // non-pointerlike operation
	}
	if dst == 0 || ptr == 0 {
		panic(fmt.Sprintf("ill-typed load dst=n%d src=n%d", dst, ptr))
	}
	for i := uint32(0); i < sizeof; i++ {
		a.nodes[ptr].fly_solve = append(a.nodes[ptr].fly_solve, &loadRule{offset, dst})
		offset++
		dst++
	}
}

// genOffsetAddr generates constraints for a 'v=ptr.field' (FieldAddr)
// or 'v=ptr[*]' (IndexAddr) instruction v.
func (a *analysis) genOffsetAddr(dst nodeid, ptr nodeid, offset uint32) {

	if offset == 0 {
		// Simplify  dst = &src->f0
		//       to  dst = src
		// (NB: this optimisation is defeated by the identity
		// field prepended to struct and array objects.)
		a.addflow(dst, ptr, 1, nil)
	} else {
		a.nodes[ptr].fly_solve = append(a.nodes[ptr].fly_solve, &offsetAddrRule{offset, dst})
	}
}

// genStore generates constraints for *(ptr + offset) = val.
func (a *analysis) genStore(ptr nodeid, src nodeid, offset, sizeof uint32) {

	if src == 0 {
		return // store of non-pointerlike value
	}
	if src == 0 && ptr == 0 {
		return // non-pointerlike operation
	}
	if src == 0 || ptr == 0 {
		panic(fmt.Sprintf("ill-typed store dst=n%d src=n%d", ptr, src))
	}
	for i := uint32(0); i < sizeof; i++ {
		a.nodes[ptr].fly_solve = append(a.nodes[ptr].fly_solve, &storeRule{offset, src})
		offset++
		src++
	}
}

// genInstr generates constraints for instruction instr in context cfc.
func (a *analysis) genInstr(cfc *funcnode, instr ssa.Instruction) {
	if a.log != nil {
		var prefix string
		if val, ok := instr.(ssa.Value); ok {
			prefix = val.Name() + " = "
		}
		fmt.Fprintf(a.log, "\t instr: %s%s\n", prefix, instr)
	}

	switch instr := instr.(type) {
	case *ssa.DebugRef, *ssa.BinOp, *ssa.If, *ssa.Jump, *ssa.Range, *ssa.RunDefers:
		// do nothing.

	case *ssa.UnOp:
		switch instr.Op {
		case token.ARROW: // <-x
			tElem := instr.X.Type().Underlying().(*types.Chan).Elem()
			a.genLoad(a.valueNode(instr), a.valueNode(instr.X), 0, a.sizeof(tElem))

		case token.MUL: // *x
			a.genLoad(a.valueNode(instr), a.valueNode(instr.X), 0, a.sizeof(instr.Type()))

		default:
			// NOT, SUB, XOR: no-op.
		}

	case ssa.CallInstruction: // *ssa.Call, *ssa.Go, *ssa.Defer
		a.genCall(cfc, instr)

	case *ssa.ChangeType:
		a.addflow(a.valueNode(instr), a.valueNode(instr.X), 1, instr)

	case *ssa.Convert:
		a.genConv(instr, cfc)

	case *ssa.Extract:
		a.addflow(a.valueNode(instr),
			a.valueOffsetNode(instr.Tuple, instr.Index),
			a.sizeof(instr.Type()), instr)

	case *ssa.FieldAddr:
		a.genOffsetAddr(a.valueNode(instr), a.valueNode(instr.X),
			a.offsetOf(mustDeref(instr.X.Type()), instr.Field))

	case *ssa.IndexAddr:
		a.genOffsetAddr(a.valueNode(instr), a.valueNode(instr.X), 1)

	case *ssa.Field:
		a.addflow(a.valueNode(instr),
			a.valueOffsetNode(instr.X, instr.Field),
			a.sizeof(instr.Type()), instr)

	case *ssa.Index:
		_, isstring := typeparams.CoreType(instr.X.Type()).(*types.Basic)
		// the flow of string elem does not make sense
		// string is a global const
		// ssaValue "abc" ==> create global obj obj_str
		// set this ssaValue a local node n points to obj_str
		// lay out
		// 		| n |
		// on the contrast, an array is a local obj
		// layout
		//		| inode | data |
		// thus, if addflow for string as array index, out of bound
		if !isstring {
			a.addflow(a.valueNode(instr), 1+a.valueNode(instr.X), a.sizeof(instr.Type()), instr)
		}
	case *ssa.Select:
		recv := a.valueOffsetNode(instr, 2) // instr : (index, recvOk, recv0, ... recv_n-1)
		for _, st := range instr.States {
			elemSize := a.sizeof(st.Chan.Type().Underlying().(*types.Chan).Elem())
			switch st.Dir {
			case types.RecvOnly:
				a.genLoad(recv, a.valueNode(st.Chan), 0, elemSize)
				recv += nodeid(elemSize)

			case types.SendOnly:
				a.genStore(a.valueNode(st.Chan), a.valueNode(st.Send), 0, elemSize)
			}
		}

	case *ssa.Return:
		results := a.funcResults(cfc.obj)
		for _, r := range instr.Results {
			sz := a.sizeof(r.Type())
			a.addflow(results, a.valueNode(r), sz, nil)
			results += nodeid(sz)
		}

	case *ssa.Send:
		a.genStore(a.valueNode(instr.Chan), a.valueNode(instr.X), 0, a.sizeof(instr.X.Type()))

	case *ssa.Store:
		a.genStore(a.valueNode(instr.Addr), a.valueNode(instr.Val), 0, a.sizeof(instr.Val.Type()))

	case *ssa.Alloc, *ssa.MakeSlice, *ssa.MakeChan, *ssa.MakeMap, *ssa.MakeInterface:
		v := instr.(ssa.Value)
		a.nodes[a.valueNode(v)].pts.add(a.objectNode(cfc, v))
		a.worklist.add(a.valueNode(v))

	case *ssa.ChangeInterface:
		a.addflow(a.valueNode(instr), a.valueNode(instr.X), 1, instr)

	case *ssa.TypeAssert:
		a.typeAssert(instr.AssertedType, a.valueNode(instr), a.valueNode(instr.X), true)

	case *ssa.Slice:
		a.addflow(a.valueNode(instr), a.valueNode(instr.X), 1, instr)

	case *ssa.SliceToArrayPointer:
		// Going from a []T to a *[k]T (for some k) is a single `dst = src` constraint.
		// Both []T and *[k]T are modelled as an *IdArrayT where IdArrayT is the identity
		// node for an array of type T, i.e `type IdArrayT struct{elem T}`.
		a.addflow(a.valueNode(instr), a.valueNode(instr.X), 1, instr)

	case *ssa.Phi:
		sz := a.sizeof(instr.Type())
		for _, e := range instr.Edges {
			a.addflow(a.valueNode(instr), a.valueNode(e), sz, instr)
		}

	case *ssa.MakeClosure:
		fn := instr.Fn.(*ssa.Function)
		a.addflow(a.valueNode(instr), a.valueNode(fn), 1, instr)
		// Free variables are treated like global variables.
		for i, b := range instr.Bindings {
			a.addflow(a.valueNode(fn.FreeVars[i]), a.valueNode(b), a.sizeof(b.Type()), instr)
		}

	case *ssa.Next:
		if !instr.IsString { // map
			// Assumes that Next is always directly applied to a Range result.
			theMap := instr.Iter.(*ssa.Range).X
			tMap := theMap.Type().Underlying().(*types.Map)

			ksize := a.sizeof(tMap.Key())
			vsize := a.sizeof(tMap.Elem())

			// The k/v components of the Next tuple may each be invalid.
			tTuple := instr.Type().(*types.Tuple)

			// Load from the map's (k,v) into the tuple's (ok, k, v).
			osrc := uint32(0) // offset within map object
			odst := uint32(1) // offset within tuple (initially just after 'ok bool')
			sz := uint32(0)   // amount to copy

			// Is key valid?
			if tTuple.At(1).Type() != tInvalid {
				sz += ksize
			} else {
				odst += ksize
				osrc += ksize
			}

			// Is value valid?
			if tTuple.At(2).Type() != tInvalid {
				sz += vsize
			}

			a.genLoad(a.valueNode(instr)+nodeid(odst), a.valueNode(theMap), osrc, sz)
		}

	case *ssa.Lookup:
		if tMap, ok := instr.X.Type().Underlying().(*types.Map); ok {
			// CommaOk can be ignored: field 0 is a no-op.
			ksize := a.sizeof(tMap.Key())
			vsize := a.sizeof(tMap.Elem())
			a.genLoad(a.valueNode(instr), a.valueNode(instr.X), ksize, vsize)
		}

	case *ssa.MapUpdate:
		tmap := instr.Map.Type().Underlying().(*types.Map)
		ksize := a.sizeof(tmap.Key())
		vsize := a.sizeof(tmap.Elem())
		a.genStore(a.valueNode(instr.Map), a.valueNode(instr.Key), 0, ksize)
		a.genStore(a.valueNode(instr.Map), a.valueNode(instr.Value), ksize, vsize)

	case *ssa.Panic:
		a.addflow(a.panicNode, a.valueNode(instr.X), 1, nil)

	default:
		panic(fmt.Sprintf("unimplemented: %T", instr))
	}
}

// genFunc generates rules for function fn.
func (a *analysis) genFunc(cfc *funcnode) {
	fn := cfc.fn

	if a.log != nil {
		fmt.Fprintln(a.log, "\tCreating nodes for local values of", cfc.func_context, cfc.fn.Name())
	}
	if fn.TypeParams().Len() > 0 && len(fn.TypeArgs()) == 0 {
		return
	}

	if strings.HasPrefix(fn.Synthetic, "instantiation wrapper ") {
		return
	}
	// Each time we analyze a new func with context, we allocate a new buffer
	a.localval = make(map[ssa.Value]nodeid)
	a.localobj = make(map[ssa.Value]nodeid)

	// The value nodes for the params are in the func object block, which should be allocated before.
	// a cfc indicate a context, so all local values or allocated objects are also context sensitive.
	params := a.funcParams(cfc.obj)
	for _, p := range fn.Params {
		a.setValueNode(p, params, cfc)
		params += nodeid(a.sizeof(p.Type()))
	}

	// Create value nodes for all value instructions
	// since SSA may contain forward references.
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.Range:
			// value defined instr, but it doesnt matter, just do nothing

			case ssa.Value:
				var comment string
				if a.log != nil {
					comment = instr.Name()
				}
				id := a.addNodes(instr.Type(), comment)
				a.setValueNode(instr, id, cfc)
			}
		}
	}

	// Generate constraints for each IR instructions.
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			a.genInstr(cfc, instr)
		}
	}

	// clear buffer
	a.localval = nil
	a.localobj = nil
}
