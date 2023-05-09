package pa

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/container/intsets"
	"golang.org/x/tools/go/ssa"
)

// indicate kind of special objects
const (
	otTagged   = 1 // possible runtime object for an interface
	otFunction = 2 // function object
)

// continuous block of nodes, denoting an object to which a pointer-like points
type object struct {
	tags uint32

	// number of nodes this object contains
	size uint32

	// the actual data this object denotes
	data interface{}

	// the func containing this objects with context-sensitivity applied
	funcn *funcnode
}

type nodeset struct {
	intsets.Sparse
}

func (ns *nodeset) add(n nodeid) bool {
	return ns.Sparse.Insert(int(n))
}

func (ns *nodeset) addAll(ns_add *nodeset) bool {
	return ns.UnionWith(&ns_add.Sparse)
}

// nodeid denotes a node
type nodeid uint32

// A node denotes a pointer-like value or the object they points to
type node struct {
	// a non-nil obj denotes this node is the start of this object
	obj *object

	typ types.Type

	sub_element *subEleInfo

	fly_solve []rule  // on-the-fly-solved rule attached to this node
	flow_to   nodeset // all pointer-like valuenode it may flow to
	pts       nodeset // pt(n)
	prev_pts  nodeset // pt(n) in previous iteration, for difference propagation
}

// A subEleInfo describes one subelement (node) of the flattening-out
// of a type T: the subelement's type and its path from the root of T.
//
// For example, for this type:
//
//	type line struct{ points []struct{x, y int} }
//
// flatten() of the inner struct yields the following []subEleInfo:
//
//	struct{ x, y int }                      ""
//	int                                     ".x"
//	int                                     ".y"
//
// and flatten(line) yields:
//
//	struct{ points []struct{x, y int} }     ""
//	struct{ x, y int }                      ".points[*]"
//	int                                     ".points[*].x
//	int                                     ".points[*].y"
type subEleInfo struct {
	typ types.Type

	op   interface{} // *Array: true; *Tuple: int; *Struct: *types.Var; *Named: nil
	tail *subEleInfo
}

// ---------- Node creation ----------

// nextNode returns the index of the next unused node.
func (a *analysis) nextNode() nodeid {
	return nodeid(len(a.nodes))
}

func (a *analysis) addNodes(typ types.Type, comment string) nodeid {
	id := a.nextNode()
	for _, fi := range a.flatten(typ) {
		a.addOneNode(fi.typ, comment, fi)
	}
	if id == a.nextNode() {
		return 0 // type contained no pointers
	}
	return id
}

// addOneNode creates a single node with type typ, and returns its id.
//
// typ should generally be scalar (except for tagged.T nodes
// and struct/array identity nodes).  Use addNodes for non-scalar types.
//
// comment explains the origin of the nodes, as a debugging aid.
// subelement indicates the subelement, e.g. ".a.b[*].c".
func (a *analysis) addOneNode(typ types.Type, comment string, subelement *subEleInfo) nodeid {
	id := a.nextNode()
	a.nodes = append(a.nodes, &node{typ: typ, sub_element: subelement})
	if a.log != nil {
		fmt.Fprintf(a.log, "\tcreate n%d %s for %s%s\n",
			id, typ, comment, subelement.path())
	}
	return id
}

// setValueNode associates node id with the value v.
// cgn identifies the context iff v is a local variable.
func (a *analysis) setValueNode(v ssa.Value, id nodeid, cgn *cgnode) {
	if cgn != nil {
		a.localval[v] = id
	} else {
		a.globalval[v] = id
	}
	if a.log != nil {
		fmt.Fprintf(a.log, "\tval[%s] = n%d  (%T)\n", v.Name(), id, v)
	}
}

// endObject marks the end of a sequence of calls to addNodes denoting
// a single object allocation.
//
// obj is the start node of the object, from a prior call to nextNode.
// Its size, flags and optional data will be updated.
func (a *analysis) endObject(obj nodeid, func_node *funcnode, data interface{}) *object {
	// Ensure object is non-empty by padding;
	// the pad will be the object node.
	size := uint32(a.nextNode() - obj)
	if size == 0 {
		a.addOneNode(tInvalid, "padding", nil)
	}
	objNode := a.nodes[obj]
	o := &object{
		size:  size, // excludes padding
		funcn: func_node,
		data:  data,
	}
	objNode.obj = o

	return o
}

// makeTagged creates a tagged object of type typ.
func (a *analysis) makeTagged(typ types.Type, func_node *funcnode, data interface{}) nodeid {
	obj := a.addOneNode(typ, "tagged.T", nil) // NB: type may be non-scalar!
	a.addNodes(typ, "tagged.v")
	a.endObject(obj, func_node, data).tags |= otTagged
	return obj
}

// valueNode returns the id of the value node for v, creating it (and
// the association) as needed.  It may return zero for uninteresting
// values containing no pointers.
func (a *analysis) valueNode(v ssa.Value) nodeid {
	// Value nodes for locals are created en masse by genFunc.
	if id, ok := a.localval[v]; ok {
		return id
	}

	// Value nodes for globals are created on demand.
	id, ok := a.globalval[v]
	if !ok {
		var comment string
		if a.log != nil {
			comment = v.String()
		}
		id = a.addNodes(v.Type(), comment)
		if obj := a.objectNode(nil, v); obj != 0 {
			a.addressOf(v.Type(), id, obj)
		}
		a.setValueNode(v, id, nil)
	}
	return id
}

func (a *analysis) objectNode(func_node *funcnode, v ssa.Value) nodeid {
	switch v.(type) {
	case *ssa.Global, *ssa.Function, *ssa.Const, *ssa.FreeVar:
		// Global object.
		obj, ok := a.globalobj[v]
		if !ok {
			switch v := v.(type) {
			case *ssa.Global:
				obj = a.nextNode()
				a.addNodes(mustDeref(v.Type()), "global")
				a.endObject(obj, nil, v)

			case *ssa.Function:
				obj = a.makeFunctionObject(v, nil)

			case *ssa.Const:
				// not addressable

			case *ssa.FreeVar:
				// not addressable
			}

			if a.log != nil {
				fmt.Fprintf(a.log, "\tglobalobj[%s] = n%d\n", v, obj)
			}
			a.globalobj[v] = obj
		}
		return obj
	}

	// Local object.
	obj, ok := a.localobj[v]
	if !ok {
		switch v := v.(type) {
		case *ssa.Alloc:
			obj = a.nextNode()
			a.addNodes(mustDeref(v.Type()), "alloc")
			a.endObject(obj, func_node, v)

		case *ssa.MakeSlice:
			obj = a.nextNode()
			a.addNodes(sliceToArray(v.Type()), "makeslice")
			a.endObject(obj, func_node, v)

		case *ssa.MakeChan:
			obj = a.nextNode()
			a.addNodes(v.Type().Underlying().(*types.Chan).Elem(), "makechan")
			a.endObject(obj, func_node, v)

		case *ssa.MakeMap:
			obj = a.nextNode()
			tmap := v.Type().Underlying().(*types.Map)
			a.addNodes(tmap.Key(), "makemap.key")
			a.addNodes(tmap.Elem(), "makemap.value")
			a.endObject(obj, func_node, v)

		case *ssa.MakeInterface:
			tConc := v.X.Type()
			obj = a.makeTagged(tConc, func_node, v)

			// Copy the value into it, if nontrivial.
			if x := a.valueNode(v.X); x != 0 {
				a.copy(obj+1, x, a.sizeof(tConc))
			}

		case *ssa.FieldAddr:
			if xobj := a.objectNode(func_node, v.X); xobj != 0 {
				obj = xobj + nodeid(a.offsetOf(mustDeref(v.X.Type()), v.Field))
			}

		case *ssa.IndexAddr:
			if xobj := a.objectNode(func_node, v.X); xobj != 0 {
				obj = xobj + 1
			}

		case *ssa.Slice:
			obj = a.objectNode(func_node, v.X)

		case *ssa.Convert:
			// TODO(adonovan): opt: handle these cases too:
			// - unsafe.Pointer->*T conversion acts like Alloc
			// - string->[]byte/[]rune conversion acts like MakeSlice
		}

		if a.log != nil {
			fmt.Fprintf(a.log, "\tlocalobj[%s] = n%d\n", v.Name(), obj)
		}
		a.localobj[v] = obj
	}
	return obj
}

// valueOffsetNode ascertains the node for tuple/struct value v,
// then returns the node for its subfield #index.
func (a *analysis) valueOffsetNode(v ssa.Value, index int) nodeid {
	id := a.valueNode(v)
	if id == 0 {
		panic(fmt.Sprintf("cannot offset within n0: %s = %s", v.Name(), v))
	}
	return id + nodeid(a.offsetOf(v.Type(), index))
}

// isTaggedObject reports whether object obj is a tagged object.
func (a *analysis) isTaggedObject(obj nodeid) bool {
	return a.nodes[obj].obj.tags&otTagged != 0
}

// taggedValue returns the dynamic type tag, the (first node of the)
// payload, and the indirect flag of the tagged object starting at id.
// Panic ensues if !isTaggedObject(id).
func (a *analysis) taggedValue(obj nodeid) (tDyn types.Type, v nodeid, indirect bool) {
	n := a.nodes[obj]
	flags := n.obj.tags
	if flags&otTagged == 0 {
		panic(fmt.Sprintf("not a tagged object: n%d", obj))
	}
	return n.typ, obj + 1, flags != 0
}

// funcParams returns the first node of the params (P) block of the
// function whose object node (obj.flags&otFunction) is id.
func (a *analysis) funcParams(id nodeid) nodeid {
	n := a.nodes[id]
	if n.obj == nil || n.obj.tags&otFunction == 0 {
		panic(fmt.Sprintf("funcParams(n%d): not a function object block", id))
	}
	return id + 1
}

// funcResults returns the first node of the results (R) block of the
// function whose object node (obj.flags&otFunction) is id.
func (a *analysis) funcResults(id nodeid) nodeid {
	n := a.nodes[id]
	if n.obj == nil || n.obj.tags&otFunction == 0 {
		panic(fmt.Sprintf("funcResults(n%d): not a function object block", id))
	}
	sig := n.typ.(*types.Signature)
	id += 1 + nodeid(a.sizeof(sig.Params()))
	if sig.Recv() != nil {
		id += nodeid(a.sizeof(sig.Recv().Type()))
	}
	return id
}

// ------------- value node related -------------

// ------------- object node related -----------

// makeFunctionObject creates and returns a new function object with context (callstring).
// related to a funcnode.
// if we can find it in csfuncobj   map[ssa.Value]map[context]nodeid, there is no need to call addreachable
func (a *analysis) makeFunctionObject(fn *ssa.Function, callersite *callsite) nodeid {
	if a.log != nil {
		fmt.Fprintf(a.log, "\t---- makeFunctionObject %s\n", fn)
	}

	// obj is the function object (identity, params, results).
	obj := a.nextNode()
	cgn := a.makeCGNode(fn, obj, callersite)
	sig := fn.Signature
	a.addOneNode(sig, "func.cgnode", nil) // (scalar with Signature type)
	if recv := sig.Recv(); recv != nil {
		a.addNodes(recv.Type(), "func.recv")
	}
	a.addNodes(sig.Params(), "func.params")
	a.addNodes(sig.Results(), "func.results")
	a.endObject(obj, cgn, fn).tags |= otFunction

	if a.log != nil {
		fmt.Fprintf(a.log, "\t----\n")
	}

	return obj
}

// ----------- util -------------

func isInterface(T types.Type) bool { return types.IsInterface(T) }

// mustDeref returns the element type of its argument, which must be a
// pointer; panic ensues otherwise.
func mustDeref(typ types.Type) types.Type {
	return typ.Underlying().(*types.Pointer).Elem()
}

// deref returns a pointer's element type; otherwise it returns typ.
func deref(typ types.Type) types.Type {
	if p, ok := typ.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return typ
}

// sizeof returns the number of pointerlike abstractions (nodes) in the type t.
func (a *analysis) sizeof(t types.Type) uint32 {
	return uint32(len(a.flatten(t)))
}

// offsetOf returns the (abstract) offset of field index within struct
// or tuple typ.
func (a *analysis) offsetOf(typ types.Type, index int) uint32 {
	var offset uint32
	switch t := typ.Underlying().(type) {
	case *types.Tuple:
		for i := 0; i < index; i++ {
			offset += a.sizeof(t.At(i).Type())
		}
	case *types.Struct:
		offset++ // the node for the struct itself
		for i := 0; i < index; i++ {
			offset += a.sizeof(t.Field(i).Type())
		}
	default:
		panic(fmt.Sprintf("offsetOf(%s : %T)", typ, typ))
	}
	return offset
}

// sliceToArray returns the type representing the arrays to which
// slice type slice points.
func sliceToArray(slice types.Type) *types.Array {
	return types.NewArray(slice.Underlying().(*types.Slice).Elem(), 1)
}

// flatten returns a list of directly contained fields in the preorder
// traversal of the type tree of t.  The resulting elements are all
// scalars (basic types or pointerlike types), except for struct/array
// "identity" nodes, whose type is that of the aggregate.
//
// reflect.Value is considered pointerlike, similar to interface{}.
//
// Callers must not mutate the result.
func (a *analysis) flatten(t types.Type) []*subEleInfo {
	fl, ok := a.flattenMemo[t]
	if !ok {
		switch t := t.(type) {
		case *types.Named:
			u := t.Underlying()
			if isInterface(u) {
				// Debuggability hack: don't remove
				// the named type from interfaces as
				// they're very verbose.
				fl = append(fl, &subEleInfo{typ: t})
			} else {
				fl = a.flatten(u)
			}

		case *types.Basic,
			*types.Signature,
			*types.Chan,
			*types.Map,
			*types.Interface,
			*types.Slice,
			*types.Pointer:
			fl = append(fl, &subEleInfo{typ: t})

		case *types.Array:
			fl = append(fl, &subEleInfo{typ: t}) // identity node
			for _, fi := range a.flatten(t.Elem()) {
				fl = append(fl, &subEleInfo{typ: fi.typ, op: true, tail: fi})
			}

		case *types.Struct:
			fl = append(fl, &subEleInfo{typ: t}) // identity node
			for i, n := 0, t.NumFields(); i < n; i++ {
				f := t.Field(i)
				for _, fi := range a.flatten(f.Type()) {
					fl = append(fl, &subEleInfo{typ: fi.typ, op: f, tail: fi})
				}
			}

		case *types.Tuple:
			// No identity node: tuples are never address-taken.
			n := t.Len()
			if n == 1 {
				// Don't add a subEleInfo link for singletons,
				// e.g. in params/results.
				fl = append(fl, a.flatten(t.At(0).Type())...)
			} else {
				for i := 0; i < n; i++ {
					f := t.At(i)
					for _, fi := range a.flatten(f.Type()) {
						fl = append(fl, &subEleInfo{typ: fi.typ, op: i, tail: fi})
					}
				}
			}

		default:
			panic(fmt.Sprintf("cannot flatten unsupported type %T", t))
		}

		a.flattenMemo[t] = fl
	}

	return fl
}
