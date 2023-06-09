package visual

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

var (
	minlen    uint    = 2
	nodesep   float64 = 0.35
	nodeshape string  = "box"
	nodestyle string  = "filled,rounded"
	rankdir   string  = "LR"
)

const tmplCluster = `{{define "cluster" -}}
    {{printf "subgraph %q {" .}}
        {{printf "%s" .Attrs.Lines}}
        {{range .Funcs}}
        {{template "clusterf" .}}
        {{- end}}
    {{println "}" }}
{{- end}}`

const tmplFCluster = `{{define "clusterf" -}}
    {{printf "subgraph %q {" .}}
        {{printf "%s" .Attrs.Lines}}
		{{template "node" .NodeI}}
        {{range .Nodes}}
        {{template "node" .}}
        {{- end}}
    {{println "}" }}
{{- end}}`

const tmplEdge = `{{define "edge" -}}
    {{printf "%q -> %q [ %s ]" .From .To .Attrs}}
{{- end}}`

const tmplNode = `{{define "node" -}}
    {{printf "%q [ %s ]" .ID .Attrs}}
{{- end}}`

const tmplGraph = `digraph gocallvis {
    label="{{.Title}}";
    labeljust="l";
    fontname="Arial";
    fontsize="14";
    rankdir="{{.Options.rankdir}}";
    bgcolor="lightgray";
    style="solid";
    penwidth="0.5";
    pad="0.0";
    nodesep="{{.Options.nodesep}}";

    node [shape="{{.Options.nodeshape}}" style="{{.Options.nodestyle}}" fillcolor="honeydew" fontname="Verdana" penwidth="1.0" margin="0.05,0.0"];
    edge [minlen="{{.Options.minlen}}"]

	{{range .Clusters}}
        {{template "cluster" .}}
    {{- end}}

    {{- range .Edges}}
    {{template "edge" .}}
    {{- end}}
}
`

// ==[ type def/func: dotCluster ]===============================================
type dotPCluster struct {
	ID    string
	Funcs []*dotFCluster
	Attrs dotAttrs
}

func NewDotPCluster(id string) *dotPCluster {
	return &dotPCluster{
		ID:    id,
		Funcs: make([]*dotFCluster, 0),
		Attrs: make(dotAttrs),
	}
}

func (c *dotPCluster) String() string {
	return fmt.Sprintf("cluster_p%s", c.ID)
}

// function

type dotFCluster struct {
	ID    string
	NodeI *dotNode // identity node
	Nodes []*dotNode
	Attrs dotAttrs
}

func NewDotFCluster(id string) *dotFCluster {
	return &dotFCluster{
		ID:    id,
		Nodes: make([]*dotNode, 0),
		Attrs: make(dotAttrs),
	}
}

func (c *dotFCluster) String() string {
	return fmt.Sprintf("cluster_f%s", c.ID)
}

// ==[ type def/func: dotNode    ]===============================================
type dotNode struct {
	ID    string
	Attrs dotAttrs
}

func (n *dotNode) String() string {
	return n.ID
}

// ==[ type def/func: dotEdge    ]===============================================
type dotEdge struct {
	From  *dotNode
	To    *dotNode // identity node of func
	Attrs dotAttrs
}

// ==[ type def/func: dotAttrs   ]===============================================
type dotAttrs map[string]string

func (p dotAttrs) List() []string {
	l := []string{}
	for k, v := range p {
		l = append(l, fmt.Sprintf("%s=%q", k, v))
	}
	return l
}

func (p dotAttrs) String() string {
	return strings.Join(p.List(), " ")
}

func (p dotAttrs) Lines() string {
	return fmt.Sprintf("%s;", strings.Join(p.List(), ";\n"))
}

// ==[ type def/func: dotGraph   ]===============================================
type dotGraph struct {
	Title    string
	Minlen   uint
	Attrs    dotAttrs
	Clusters map[string]*dotPCluster
	Edges    []*dotEdge
	Options  map[string]string
}

func (g *dotGraph) WriteDot(w io.Writer) error {
	t := template.New("dot")
	for _, s := range []string{tmplCluster, tmplNode, tmplEdge, tmplGraph, tmplFCluster} {
		if _, err := t.Parse(s); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, g); err != nil {
		return err
	}
	_, err := buf.WriteTo(w)
	return err
}

// location of dot executable for converting from .dot to .svg
// it's usually at: /usr/bin/dot
var dotSystemBinary string

// runDotToImageCallSystemGraphviz generates a SVG using the 'dot' utility, returning the filepath
func runDotToImageCallSystemGraphviz(outfname string, format string, dot []byte) (string, error) {
	if dotSystemBinary == "" {
		dot, err := exec.LookPath("dot")
		if err != nil {
			log.Fatalln("unable to find program 'dot', please install it or check your PATH")
		}
		dotSystemBinary = dot
	}

	var img string
	if outfname == "" {
		img = filepath.Join(os.TempDir(), fmt.Sprintf("go-callvis_export.%s", format))
	} else {
		img = fmt.Sprintf("%s.%s", outfname, format)
	}
	cmd := exec.Command(dotSystemBinary, fmt.Sprintf("-T%s", format), "-o", img)
	cmd.Stdin = bytes.NewReader(dot)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command '%v': %v\n%v", cmd, err, stderr.String())
	}
	return img, nil
}
