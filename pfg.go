package pa

import (
	"go/types"

	"golang.org/x/tools/container/intsets"
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

// A fieldInfo describes one subelement (node) of the flattening-out
// of a type T: the subelement's type and its path from the root of T.
//
// For example, for this type:
//
//	type line struct{ points []struct{x, y int} }
//
// flatten() of the inner struct yields the following []fieldInfo:
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
