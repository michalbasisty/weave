// Package weave is the Go port of src/: one file per TypeScript module.
package weave

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
)

// GraphVersion is GRAPH_VERSION: an index with another version is rebuilt.
const GraphVersion = 3

// Node kinds.
const (
	KindModule    = "module"
	KindEndpoint  = "endpoint"
	KindCall      = "call"
	KindModel     = "model"
	KindTable     = "table"
	KindPage      = "page"
	KindComponent = "component"
	KindTest      = "test"
	KindReq       = "req"
	KindTask      = "task"
	KindChange    = "change"
	KindFile      = "file"
)

// Edge confidence.
const (
	ConfExact    = "exact"
	ConfInferred = "inferred"
)

// The json tags document the shape the TypeScript writes; the bytes come from
// AppendJS, not encoding/json. A nil pointer or slice is a JS undefined and is
// omitted; a zero value or an empty slice is written, as JS writes 0, "" and [].
// Fields are declared in the order the TypeScript inserts them at runtime.

// GNode is a graph node.
type GNode struct {
	ID   string         `json:"id"`
	Kind string         `json:"kind"`
	Name string         `json:"name"`
	File *string        `json:"file,omitempty"`
	Line *int           `json:"line,omitempty"`
	Meta *jsjson.Object `json:"meta,omitempty"`

	// order is the key order read from a file when it is not the declared one:
	// Builder.node's `{ ...prev, ...n }` keeps prev's keys first.
	order []string
	// extra holds keys this struct does not model, so they survive a round trip.
	extra *jsjson.Object
}

// GEdge is a graph edge.
type GEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Conf string `json:"conf"`
}

// FileStats is the routes/next/trpc/openapi coverage shape.
type FileStats struct {
	Files  int      `json:"files"`
	Found  int      `json:"found"`
	Silent []string `json:"silent"`
	// Ignored is a contract this build cannot read (an OpenAPI spec in YAML), named
	// so the gap is reported rather than assumed absent.
	Ignored []string `json:"ignored,omitempty"`
}

// CallStats is coverage.calls.
type CallStats struct {
	Total    int      `json:"total"`
	Resolved int      `json:"resolved"`
	Dynamic  *int     `json:"dynamic,omitempty"`
	Silent   []string `json:"silent,omitempty"`
}

// PageStats is coverage.pages.
type PageStats struct {
	Declared int `json:"declared"`
	Mapped   int `json:"mapped"`
	Skipped  int `json:"skipped"`
}

// SpecStats is coverage.specs.
type SpecStats struct {
	Files  int      `json:"files"`
	Parsed int      `json:"parsed"`
	Silent []string `json:"silent"`
}

// PrismaStats is coverage.prisma.
type PrismaStats struct {
	Models int `json:"models"`
	Tables int `json:"tables"`
}

// Coverage is how much of the repo the extractors actually saw.
type Coverage struct {
	Routes   FileStats   `json:"routes"`
	Calls    CallStats   `json:"calls"`
	Pages    PageStats   `json:"pages"`
	Specs    SpecStats   `json:"specs"`
	Prisma   PrismaStats `json:"prisma"`
	Next     *FileStats  `json:"next,omitempty"`
	NextPage *FileStats  `json:"nextPages,omitempty"`
	Trpc     *FileStats  `json:"trpc,omitempty"`
	OpenAPI  *FileStats  `json:"openapi,omitempty"`
	Patterns *int        `json:"patterns,omitempty"`
}

// Phase is one lap of perf.Timer.
type Phase struct {
	Name string
	Ms   float64
}

// Phases is perf.phases: an object whose key order is the order the laps ran.
type Phases []Phase

// Get returns a phase's time, as phases[name] ?? 0.
func (p Phases) Get(name string) float64 {
	for _, x := range p {
		if x.Name == name {
			return x.Ms
		}
	}
	return 0
}

// Set records a lap like marks[name] = ms: a repeated name keeps its place.
func (p *Phases) Set(name string, ms float64) {
	for i := range *p {
		if (*p)[i].Name == name {
			(*p)[i].Ms = ms
			return
		}
	}
	*p = append(*p, Phase{name, ms})
}

// Perf is the phase timings of the run that produced the index.
type Perf struct {
	TotalMs float64 `json:"totalMs"`
	Phases  Phases  `json:"phases"`
	HeapMb  float64 `json:"heapMb"`
}

// Graph is .weave/graph.json.
type Graph struct {
	Version   int       `json:"version"`
	Root      string    `json:"root"`
	BuiltAt   string    `json:"builtAt"`
	FileCount int       `json:"fileCount"`
	Nodes     []*GNode  `json:"nodes"`
	Edges     []GEdge   `json:"edges"`
	Coverage  *Coverage `json:"coverage,omitempty"`
	Perf      *Perf     `json:"perf,omitempty"`
	// Endpoint names present in the previous index: lets `check` prove what a change removed.
	PrevEndpoints []string `json:"prevEndpoints,omitempty"`
}

// Ptr returns a pointer to v, for optional fields.
func Ptr[T any](v T) *T { return &v }

// Node ids, as `id` in model.ts. Methods are HTTP verbs, so ASCII upper-casing
// matches toUpperCase.
func ModuleID(n string) string { return "module:" + n }
func EndpointID(method, path string) string {
	return "endpoint:" + strings.ToUpper(method) + " " + path
}
func CallID(method, path string) string { return "call:" + strings.ToUpper(method) + " " + path }
func ModelID(n string) string           { return "model:" + n }
func TableID(n string) string           { return "table:" + n }
func PageID(route string) string        { return "page:" + route }
func TestID(file string) string         { return "test:" + file }
func ReqID(n string) string             { return "req:" + n }
func TaskID(n string) string            { return "task:" + n }
func ChangeID(n string) string          { return "change:" + n }
func FileID(n string) string            { return "file:" + n }

// ---- encoding: JSON.stringify(g) ----

type objWriter struct {
	b []byte
	n int
}

func (w *objWriter) key(k string) {
	if w.n == 0 {
		w.b = append(w.b, '{')
	} else {
		w.b = append(w.b, ',')
	}
	w.n++
	w.b = jsjson.AppendString(w.b, k)
	w.b = append(w.b, ':')
}

func (w *objWriter) str(k, v string)     { w.key(k); w.b = jsjson.AppendString(w.b, v) }
func (w *objWriter) int(k string, v int) { w.key(k); w.b = strconv.AppendInt(w.b, int64(v), 10) }
func (w *objWriter) num(k string, v float64) {
	w.key(k)
	w.b = jsjson.Append(w.b, jsjson.Num(v))
}
func (w *objWriter) strs(k string, v []string) { w.key(k); w.b = appendStrs(w.b, v) }

func (w *objWriter) end() []byte {
	if w.n == 0 {
		return append(w.b, '{', '}')
	}
	return append(w.b, '}')
}

func appendStrs(b []byte, v []string) []byte {
	b = append(b, '[')
	for i, s := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = jsjson.AppendString(b, s)
	}
	return append(b, ']')
}

// MarshalJS is JSON.stringify(g).
func (g *Graph) MarshalJS() []byte { return g.AppendJS(nil) }

// AppendJS appends JSON.stringify(g) to b.
func (g *Graph) AppendJS(b []byte) []byte {
	w := objWriter{b: b}
	w.int("version", g.Version)
	w.str("root", g.Root)
	w.str("builtAt", g.BuiltAt)
	w.int("fileCount", g.FileCount)
	w.key("nodes")
	w.b = append(w.b, '[')
	for i, n := range g.Nodes {
		if i > 0 {
			w.b = append(w.b, ',')
		}
		w.b = n.AppendJS(w.b)
	}
	w.b = append(w.b, ']')
	w.key("edges")
	w.b = append(w.b, '[')
	for i, e := range g.Edges {
		if i > 0 {
			w.b = append(w.b, ',')
		}
		w.b = e.AppendJS(w.b)
	}
	w.b = append(w.b, ']')
	if g.Coverage != nil {
		w.key("coverage")
		w.b = g.Coverage.AppendJS(w.b)
	}
	if g.Perf != nil {
		w.key("perf")
		w.b = g.Perf.AppendJS(w.b)
	}
	if g.PrevEndpoints != nil {
		w.strs("prevEndpoints", g.PrevEndpoints)
	}
	return w.end()
}

var nodeKeys = []string{"id", "kind", "name", "file", "line", "meta"}

// AppendJS appends JSON.stringify(n) to b.
func (n *GNode) AppendJS(b []byte) []byte {
	keys := n.order
	if keys == nil {
		keys = nodeKeys
	}
	w := objWriter{b: b}
	for _, k := range keys {
		switch k {
		case "id":
			w.str(k, n.ID)
		case "kind":
			w.str(k, n.Kind)
		case "name":
			w.str(k, n.Name)
		case "file":
			if n.File != nil {
				w.str(k, *n.File)
			}
		case "line":
			if n.Line != nil {
				w.int(k, *n.Line)
			}
		case "meta":
			if n.Meta != nil {
				w.key(k)
				w.b = jsjson.Append(w.b, jsjson.Obj(n.Meta))
			}
		default:
			if v, ok := n.extra.Get(k); ok {
				w.key(k)
				w.b = jsjson.Append(w.b, v)
			}
		}
	}
	return w.end()
}

// AppendJS appends JSON.stringify(e) to b.
func (e GEdge) AppendJS(b []byte) []byte {
	w := objWriter{b: b}
	w.str("from", e.From)
	w.str("to", e.To)
	w.str("kind", e.Kind)
	w.str("conf", e.Conf)
	return w.end()
}

func (s *FileStats) appendJS(b []byte) []byte {
	w := objWriter{b: b}
	w.int("files", s.Files)
	w.int("found", s.Found)
	w.strs("silent", orEmpty(s.Silent))
	if len(s.Ignored) != 0 {
		w.strs("ignored", s.Ignored)
	}
	return w.end()
}

// AppendJS appends JSON.stringify(c) to b.
func (c *Coverage) AppendJS(b []byte) []byte {
	w := objWriter{b: b}
	w.key("routes")
	w.b = c.Routes.appendJS(w.b)

	w.key("calls")
	cw := objWriter{b: w.b}
	cw.int("total", c.Calls.Total)
	cw.int("resolved", c.Calls.Resolved)
	if c.Calls.Dynamic != nil {
		cw.int("dynamic", *c.Calls.Dynamic)
	}
	if c.Calls.Silent != nil {
		cw.strs("silent", c.Calls.Silent)
	}
	w.b = cw.end()

	w.key("pages")
	pw := objWriter{b: w.b}
	pw.int("declared", c.Pages.Declared)
	pw.int("mapped", c.Pages.Mapped)
	pw.int("skipped", c.Pages.Skipped)
	w.b = pw.end()

	w.key("specs")
	sw := objWriter{b: w.b}
	sw.int("files", c.Specs.Files)
	sw.int("parsed", c.Specs.Parsed)
	sw.strs("silent", orEmpty(c.Specs.Silent))
	w.b = sw.end()

	w.key("prisma")
	mw := objWriter{b: w.b}
	mw.int("models", c.Prisma.Models)
	mw.int("tables", c.Prisma.Tables)
	w.b = mw.end()

	for _, f := range []struct {
		k string
		s *FileStats
	}{{"next", c.Next}, {"nextPages", c.NextPage}, {"trpc", c.Trpc}, {"openapi", c.OpenAPI}} {
		if f.s != nil {
			w.key(f.k)
			w.b = f.s.appendJS(w.b)
		}
	}
	if c.Patterns != nil {
		w.int("patterns", *c.Patterns)
	}
	return w.end()
}

// AppendJS appends JSON.stringify(p) to b.
func (p *Perf) AppendJS(b []byte) []byte {
	w := objWriter{b: b}
	w.num("totalMs", p.TotalMs)
	w.key("phases")
	pw := objWriter{b: w.b}
	for _, ph := range p.Phases {
		pw.num(ph.Name, ph.Ms)
	}
	w.b = pw.end()
	w.num("heapMb", p.HeapMb)
	return w.end()
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---- decoding: JSON.parse(text) as Graph ----

type decoder struct{ err error }

func (d *decoder) fail(path, want string) {
	if d.err == nil {
		d.err = fmt.Errorf("graph.json: %s is not %s", path, want)
	}
}

func (d *decoder) obj(v jsjson.Value, path string) *jsjson.Object {
	if v.Kind() != jsjson.KindObject {
		d.fail(path, "an object")
		return &jsjson.Object{}
	}
	return v.Object()
}

func (d *decoder) str(o *jsjson.Object, k, path string) string {
	v, _ := o.Get(k)
	if v.Kind() != jsjson.KindString {
		d.fail(path+"."+k, "a string")
	}
	return v.Str()
}

func (d *decoder) intOf(v jsjson.Value, path string) int {
	if v.Kind() != jsjson.KindNumber {
		d.fail(path, "a number")
		return 0
	}
	n, err := strconv.Atoi(v.Lexeme())
	if err != nil {
		d.fail(path, "an integer")
	}
	return n
}

func (d *decoder) int(o *jsjson.Object, k, path string) int {
	v, _ := o.Get(k)
	return d.intOf(v, path+"."+k)
}

func (d *decoder) optInt(o *jsjson.Object, k, path string) *int {
	v, ok := o.Get(k)
	if !ok {
		return nil
	}
	n := d.intOf(v, path+"."+k)
	return &n
}

func (d *decoder) num(o *jsjson.Object, k, path string) float64 {
	v, _ := o.Get(k)
	if v.Kind() != jsjson.KindNumber {
		d.fail(path+"."+k, "a number")
	}
	return v.Float()
}

func (d *decoder) strs(o *jsjson.Object, k, path string) []string {
	v, ok := o.Get(k)
	if !ok {
		return nil
	}
	if v.Kind() != jsjson.KindArray {
		d.fail(path+"."+k, "an array")
		return nil
	}
	out := make([]string, len(v.Array()))
	for i, x := range v.Array() {
		if x.Kind() != jsjson.KindString {
			d.fail(fmt.Sprintf("%s.%s[%d]", path, k, i), "a string")
		}
		out[i] = x.Str()
	}
	return out
}

// DecodeGraph turns a parsed graph.json into a Graph without checking its version.
func DecodeGraph(v jsjson.Value) (*Graph, error) {
	d := &decoder{}
	o := d.obj(v, "graph")
	g := &Graph{
		Version:   d.int(o, "version", "graph"),
		Root:      d.str(o, "root", "graph"),
		BuiltAt:   d.str(o, "builtAt", "graph"),
		FileCount: d.int(o, "fileCount", "graph"),
	}
	nodes, _ := o.Get("nodes")
	g.Nodes = make([]*GNode, 0, len(nodes.Array()))
	for i, x := range nodes.Array() {
		g.Nodes = append(g.Nodes, d.node(x, fmt.Sprintf("nodes[%d]", i)))
	}
	edges, _ := o.Get("edges")
	g.Edges = make([]GEdge, 0, len(edges.Array()))
	for i, x := range edges.Array() {
		path := fmt.Sprintf("edges[%d]", i)
		e := d.obj(x, path)
		g.Edges = append(g.Edges, GEdge{From: d.str(e, "from", path), To: d.str(e, "to", path), Kind: d.str(e, "kind", path), Conf: d.str(e, "conf", path)})
	}
	if c, ok := o.Get("coverage"); ok {
		g.Coverage = d.coverage(d.obj(c, "coverage"))
	}
	if p, ok := o.Get("perf"); ok {
		po := d.obj(p, "perf")
		g.Perf = &Perf{TotalMs: d.num(po, "totalMs", "perf"), HeapMb: d.num(po, "heapMb", "perf")}
		ph, _ := po.Get("phases")
		phases := d.obj(ph, "perf.phases")
		g.Perf.Phases = Phases{}
		for i := 0; i < phases.Len(); i++ {
			k, _ := phases.At(i)
			g.Perf.Phases = append(g.Perf.Phases, Phase{k, d.num(phases, k, "perf.phases")})
		}
	}
	g.PrevEndpoints = d.strs(o, "prevEndpoints", "graph")
	if d.err != nil {
		return nil, d.err
	}
	return g, nil
}

func (d *decoder) node(v jsjson.Value, path string) *GNode {
	o := d.obj(v, path)
	n := &GNode{ID: d.str(o, "id", path), Kind: d.str(o, "kind", path), Name: d.str(o, "name", path)}
	if _, ok := o.Get("file"); ok {
		n.File = Ptr(d.str(o, "file", path))
	}
	n.Line = d.optInt(o, "line", path)
	if m, ok := o.Get("meta"); ok {
		n.Meta = d.obj(m, path+".meta")
	}
	canonical := true
	j := 0
	for _, k := range o.Keys() {
		known := false
		for j < len(nodeKeys) && !known {
			known = nodeKeys[j] == k
			j++
		}
		if !known {
			canonical = false
			break
		}
	}
	if !canonical {
		n.order = append([]string(nil), o.Keys()...)
		for i := 0; i < o.Len(); i++ {
			k, x := o.At(i)
			if !strings.Contains(" id kind name file line meta ", " "+k+" ") {
				if n.extra == nil {
					n.extra = &jsjson.Object{}
				}
				n.extra.Set(k, x)
			}
		}
	}
	return n
}

func (d *decoder) fileStats(v jsjson.Value, path string) FileStats {
	o := d.obj(v, path)
	return FileStats{Files: d.int(o, "files", path), Found: d.int(o, "found", path), Silent: d.strs(o, "silent", path), Ignored: d.strs(o, "ignored", path)}
}

func (d *decoder) coverage(o *jsjson.Object) *Coverage {
	get := func(k string) jsjson.Value { v, _ := o.Get(k); return v }
	c := &Coverage{Routes: d.fileStats(get("routes"), "coverage.routes")}
	calls := d.obj(get("calls"), "coverage.calls")
	c.Calls = CallStats{
		Total:    d.int(calls, "total", "coverage.calls"),
		Resolved: d.int(calls, "resolved", "coverage.calls"),
		Dynamic:  d.optInt(calls, "dynamic", "coverage.calls"),
		Silent:   d.strs(calls, "silent", "coverage.calls"),
	}
	pages := d.obj(get("pages"), "coverage.pages")
	c.Pages = PageStats{d.int(pages, "declared", "coverage.pages"), d.int(pages, "mapped", "coverage.pages"), d.int(pages, "skipped", "coverage.pages")}
	specs := d.obj(get("specs"), "coverage.specs")
	c.Specs = SpecStats{d.int(specs, "files", "coverage.specs"), d.int(specs, "parsed", "coverage.specs"), d.strs(specs, "silent", "coverage.specs")}
	prisma := d.obj(get("prisma"), "coverage.prisma")
	c.Prisma = PrismaStats{d.int(prisma, "models", "coverage.prisma"), d.int(prisma, "tables", "coverage.prisma")}
	for _, f := range []struct {
		k string
		p **FileStats
	}{{"next", &c.Next}, {"nextPages", &c.NextPage}, {"trpc", &c.Trpc}, {"openapi", &c.OpenAPI}} {
		if v, ok := o.Get(f.k); ok {
			s := d.fileStats(v, "coverage."+f.k)
			*f.p = &s
		}
	}
	c.Patterns = d.optInt(o, "patterns", "coverage")
	return c
}
