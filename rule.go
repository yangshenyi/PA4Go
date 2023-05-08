package pa

import "go/types"

type rule interface {
	addflow(a *analysis, delta *nodeset)
	String() string
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
type typeFilterConstraint struct {
	typ types.Type // an interface type
	d   nodeid
}

// d = s.(typ)  where typ is a concrete type
// A complex constraint attached to src (the interface).
//
// If exact, only tagged objects identical to typ are untagged.
// If !exact, tagged objects assignable to typ are untagged too.
// The latter is needed for various reflect operators, e.g. Send.
//
// This entails a representation change:
// pts(src) contains tagged objects,
// pts(dst) contains their payloads.
type untagConstraint struct {
	typ   types.Type // a concrete type
	d     nodeid
	exact bool
}

// src.method(params...)
// A complex constraint attached to iface.
type invokeConstraint struct {
	method *types.Func // the abstract method
	iface  nodeid      // (ptr) the interface
	params nodeid      // the start of the identity/params/results block
}
