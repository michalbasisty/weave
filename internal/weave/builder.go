package weave

import (
	"time"

	"github.com/michalbasisty/weave/internal/jsjson"
)

// Builder is model.ts's Builder: nodes keep first-insertion order, a later node
// with the same id only fills in a missing file, and edges are de-duplicated on
// `${from}|${to}|${kind}` exactly as the TypeScript keys them.
type Builder struct {
	nodes []*GNode
	byID  map[string]int
	edges []GEdge
	seen  map[string]struct{}
}

// NewBuilder returns an empty builder.
func NewBuilder() *Builder {
	return &Builder{byID: map[string]int{}, seen: map[string]struct{}{}}
}

// Node adds n unless its id exists. An existing node without a file is replaced
// by `{ ...prev, ...n }`: prev's keys first, n's values win.
func (b *Builder) Node(n *GNode) string {
	i, ok := b.byID[n.ID]
	if !ok {
		b.byID[n.ID] = len(b.nodes)
		b.nodes = append(b.nodes, n)
		return n.ID
	}
	prev := b.nodes[i]
	if (prev.File == nil || *prev.File == "") && n.File != nil && *n.File != "" {
		merged := *prev
		merged.ID, merged.Kind, merged.Name, merged.File = n.ID, n.Kind, n.Name, n.File
		if n.Line != nil {
			merged.Line = n.Line
		}
		if n.Meta != nil {
			merged.Meta = n.Meta
		}
		merged.order = mergedOrder(prev, n)
		b.nodes[i] = &merged
	}
	return n.ID
}

func presentKeys(n *GNode) []string {
	keys := []string{"id", "kind", "name"}
	if n.File != nil {
		keys = append(keys, "file")
	}
	if n.Line != nil {
		keys = append(keys, "line")
	}
	if n.Meta != nil {
		keys = append(keys, "meta")
	}
	return keys
}

// mergedOrder is the key order of `{ ...prev, ...n }`, or nil when it is the
// declared order.
func mergedOrder(prev, n *GNode) []string {
	keys := presentKeys(prev)
	for _, k := range presentKeys(n) {
		found := false
		for _, x := range keys {
			found = found || x == k
		}
		if !found {
			keys = append(keys, k)
		}
	}
	j := 0
	for _, k := range keys {
		for j < len(nodeKeys) && nodeKeys[j] != k {
			j++
		}
		if j == len(nodeKeys) {
			return keys
		}
	}
	return nil
}

// Edge adds an exact edge.
func (b *Builder) Edge(from, to, kind string) { b.EdgeConf(from, to, kind, ConfExact) }

// EdgeConf adds an edge unless it is a self loop or already present.
func (b *Builder) EdgeConf(from, to, kind, conf string) {
	if from == to {
		return
	}
	key := from + "|" + to + "|" + kind
	if _, ok := b.seen[key]; ok {
		return
	}
	b.seen[key] = struct{}{}
	b.edges = append(b.edges, GEdge{From: from, To: to, Kind: kind, Conf: conf})
}

// Finish is the graph built so far.
func (b *Builder) Finish(root string, fileCount int) *Graph {
	return &Graph{
		Version:   GraphVersion,
		Root:      root,
		BuiltAt:   isoNow(),
		FileCount: fileCount,
		Nodes:     b.nodes,
		Edges:     b.edges,
	}
}

// isoNow is new Date().toISOString().
func isoNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// node is a GNode literal; empty optional fields are left out by the callers.
func fileNode(rel string) *GNode {
	return &GNode{ID: FileID(rel), Kind: KindFile, Name: rel, File: Ptr(rel)}
}

// strMeta builds a meta object from key/value string pairs, in order.
func strMeta(kv ...string) *jsjson.Object {
	o := &jsjson.Object{}
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i], jsjson.Str(kv[i+1]))
	}
	return o
}
