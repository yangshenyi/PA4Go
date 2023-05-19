package visual

import (
	"bytes"
	"fmt"
	"go/build"
	"go/types"
	"log"
	"path/filepath"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

const debug_flag = false

func isSynthetic(edge *callgraph.Edge) bool {
	return edge.Caller.Func.Pkg == nil || edge.Callee.Func.Synthetic != ""
}

func inStd(node *callgraph.Node) bool {
	pkg, _ := build.Import(node.Func.Pkg.Pkg.Path(), "", 0)
	return pkg.Goroot
}

func logf(f string, a ...interface{}) {
	if debug_flag {
		log.Printf(f, a...)
	}
}

/*
pkg		pclu

func	fclu

call	node

call : node --> func
*/
func PrintOutput(
	prog *ssa.Program,
	mainPkg *ssa.Package,
	cg *callgraph.Graph,
	focusPkg *types.Package,
	nostd,
	nointer bool,
) ([]byte, error) {

	pkgs := make(map[string]*dotPCluster, 0)

	var (
		edges []*dotEdge
	)

	// function
	nodeMap := make(map[string]*dotFCluster)
	edgeMap := make(map[string]*dotEdge)

	cg.DeleteSyntheticNodes()

	logf("no std packages: %v", nostd)

	var isFocused = func(edge *callgraph.Edge) bool {
		caller := edge.Caller
		callee := edge.Callee
		if focusPkg != nil && (caller.Func.Pkg.Pkg.Path() == focusPkg.Path() || callee.Func.Pkg.Pkg.Path() == focusPkg.Path()) {
			return true
		}
		fromFocused := false
		toFocused := false
		for _, e := range caller.In {
			if !isSynthetic(e) && focusPkg != nil &&
				e.Caller.Func.Pkg.Pkg.Path() == focusPkg.Path() {
				fromFocused = true
				break
			}
		}
		for _, e := range callee.Out {
			if !isSynthetic(e) && focusPkg != nil &&
				e.Callee.Func.Pkg.Pkg.Path() == focusPkg.Path() {
				toFocused = true
				break
			}
		}
		if fromFocused && toFocused {
			logf("edge semi-focus: %s", edge)
			return true
		}
		return false
	}
	_ = isFocused

	var isInter = func(edge *callgraph.Edge) bool {
		//caller := edge.Caller
		callee := edge.Callee
		if callee.Func.Object() != nil && !callee.Func.Object().Exported() {
			return true
		}
		return false
	}

	count := 0
	err := callgraph.GraphVisitEdges(cg, func(edge *callgraph.Edge) error {
		count++

		caller := edge.Caller
		callee := edge.Callee

		posCaller := prog.Fset.Position(caller.Func.Pos())
		posCallee := prog.Fset.Position(callee.Func.Pos())
		posEdge := prog.Fset.Position(edge.Pos())
		//fileCaller := fmt.Sprintf("%s:%d", posCaller.Filename, posCaller.Line)
		filenameCaller := filepath.Base(posCaller.Filename)

		// omit synthetic calls
		if isSynthetic(edge) {
			return nil
		}

		callerPkg := caller.Func.Pkg.Pkg
		calleePkg := callee.Func.Pkg.Pkg

		// omit std call
		if nostd && inStd(caller) {
			return nil
		}

		// omit inter
		if nointer && isInter(edge) {
			return nil
		}

		//var buf bytes.Buffer
		//data, _ := json.MarshalIndent(caller.Func, "", " ")
		//logf("call node: %s -> %s\n %v", caller, callee, string(data))
		logf("call node: %s -> %s (%s -> %s) %v\n", caller.Func.Pkg, callee.Func.Pkg, caller, callee, filenameCaller)

		// 定义一个函数
		var sprintNode = func(node *callgraph.Node, isCaller bool) *dotFCluster {
			// only once
			key := node.Func.String()
			nodeTooltip := ""

			fileCaller := fmt.Sprintf("%s:%d", filepath.Base(posCaller.Filename), posCaller.Line)
			fileCallee := fmt.Sprintf("%s:%d", filepath.Base(posCallee.Filename), posCallee.Line)

			if isCaller {
				nodeTooltip = fmt.Sprintf("%s | defined in %s", node.Func.String(), fileCaller)
			} else {
				nodeTooltip = fmt.Sprintf("%s | defined in %s", node.Func.String(), fileCallee)
			}

			if n, ok := nodeMap[key]; ok {
				return n
			}

			// is focused
			/*
				isFocused := focusPkg != nil &&
					node.Func.Pkg.Pkg.Path() == focusPkg.Path()
			*/
			attrs := make(dotAttrs)

			// node label
			label := node.Func.RelString(node.Func.Pkg.Pkg)

			pkg, _ := build.Import(node.Func.Pkg.Pkg.Path(), "", 0)
			// set node color
			if pkg.Goroot {
				attrs["fillcolor"] = "#adedad"
			} else {
				attrs["fillcolor"] = "moccasin"
			}

			attrs["label"] = label

			// func styles
			if node.Func.Parent() != nil {
				attrs["style"] = "dotted,filled"
			} else if node.Func.Object() != nil && node.Func.Object().Exported() {
				attrs["penwidth"] = "1.5"
			} else {
				attrs["penwidth"] = "0.5"
			}

			// group by pkg

			label2 := ""
			key2 := ""
			if node.Func.Pkg != nil {
				label2 = node.Func.Pkg.Pkg.Name()
				if pkg.Goroot {
					label2 = node.Func.Pkg.Pkg.Path()
				}
				key2 = node.Func.Pkg.Pkg.Path() // use it to represent unique pkg --> key for pclu
			} else if node.Func.Parent() != nil {
				label2 = node.Func.Parent().Pkg.Pkg.Name()
				key2 = node.Func.Parent().Pkg.Pkg.Path()
			}
			if _, ok := pkgs[key2]; !ok {
				pkgs[key2] = &dotPCluster{
					ID:    key2,
					Funcs: make([]*dotFCluster, 0),
					Attrs: dotAttrs{
						"penwidth":  "0.8",
						"fontsize":  "16",
						"label":     label2,
						"style":     "filled",
						"fillcolor": "lightyellow",
						"URL":       fmt.Sprintf("/?f=%s", key),
						"fontname":  "Tahoma bold",
						"tooltip":   fmt.Sprintf("package: %s", key),
						"rank":      "sink",
					},
				}
				if pkg.Goroot {
					pkgs[key2].Attrs["fillcolor"] = "#E0FFE1"
				}
			}

			attrs["tooltip"] = nodeTooltip

			n := &dotFCluster{
				ID:    node.Func.String(),
				NodeI: &dotNode{ID: fmt.Sprint("[", prog.Fset.Position(node.Func.Pos()).Line, "] ", node.Func.String())},
				Nodes: make([]*dotNode, 0),
				Attrs: attrs,
			}

			pkgs[key2].Funcs = append(pkgs[key2].Funcs, n)

			nodeMap[key] = n
			return n
		}
		callerNode := sprintNode(edge.Caller, true)
		calleeNode := sprintNode(edge.Callee, false)

		// edges
		attrs := make(dotAttrs)

		// dynamic call
		if edge.Site != nil && edge.Site.Common().StaticCallee() == nil {
			attrs["style"] = "dashed"
		}

		// go & defer calls
		switch edge.Site.(type) {
		case *ssa.Go:
			attrs["arrowhead"] = "normalnoneodot"
		case *ssa.Defer:
			attrs["arrowhead"] = "normalnoneodiamond"
		}

		// colorize calls outside focused pkg
		if focusPkg != nil &&
			(calleePkg.Path() != focusPkg.Path() || callerPkg.Path() != focusPkg.Path()) {
			attrs["color"] = "saddlebrown"
		}

		// use position in file where callee is called as tooltip for the edge
		fileEdge := fmt.Sprintf(
			"at %s:%d: calling [%s]",
			filepath.Base(posEdge.Filename),
			posEdge.Line,
			edge.Callee.Func.String(),
		)

		call_id := edge.Site.String()
		dotNode_call := &dotNode{
			ID: fmt.Sprint("[", prog.Fset.Position(edge.Pos()).Line, "] ", call_id),
			Attrs: dotAttrs{
				"penwidth":  "0.8",
				"fontsize":  "10",
				"style":     "filled",
				"fillcolor": "lightyellow",
				"fontname":  "Tahoma bold",
				"rank":      "sink",
			},
		}
		callerNode.Nodes = append(callerNode.Nodes, dotNode_call)
		// omit duplicate calls, except for tooltip enhancements
		key := fmt.Sprintf("%s = %s => %s", caller.Func, edge.Description(), callee.Func)
		if _, ok := edgeMap[key]; !ok {
			attrs["tooltip"] = fileEdge
			e := &dotEdge{
				From:  dotNode_call,
				To:    calleeNode.NodeI,
				Attrs: attrs,
			}
			edgeMap[key] = e
		} else {
			// make sure, tooltip is created correctly
			if _, okk := edgeMap[key].Attrs["tooltip"]; !okk {
				edgeMap[key].Attrs["tooltip"] = fileEdge
			} else {
				edgeMap[key].Attrs["tooltip"] = fmt.Sprintf(
					"%s\n%s",
					edgeMap[key].Attrs["tooltip"],
					fileEdge,
				)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// get edges form edgeMap
	for _, e := range edgeMap {
		e.From.Attrs["tooltip"] = fmt.Sprintf(
			"%s\n%s",
			e.From.Attrs["tooltip"],
			e.Attrs["tooltip"],
		)
		edges = append(edges, e)
	}

	logf("%d/%d edges", len(edges), count)

	title := ""
	if mainPkg != nil && mainPkg.Pkg != nil {
		title = mainPkg.Pkg.Path()
	}
	dot := &dotGraph{
		Title:    title,
		Minlen:   minlen,
		Clusters: pkgs,
		Edges:    edges,
		Options: map[string]string{
			"minlen":    fmt.Sprint(minlen),
			"nodesep":   fmt.Sprint(nodesep),
			"nodeshape": fmt.Sprint(nodeshape),
			"nodestyle": fmt.Sprint(nodestyle),
			"rankdir":   fmt.Sprint(rankdir),
		},
	}

	var buf bytes.Buffer
	if err := dot.WriteDot(&buf); err != nil {
		return nil, err
	}
	runDotToImageCallSystemGraphviz("E:\\projects\\GoVulCheckValid\\PA4Go\\hhh", "svg", buf.Bytes())
	return buf.Bytes(), nil

}
