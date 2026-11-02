package weave

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// IndexPath is <root>/.weave/graph.json.
func IndexPath(root string) string {
	return filepath.Join(root, ".weave", "graph.json")
}

// LoadGraph reads the index, refusing one written by another graph version.
// A malformed file fails with a jsjson.SyntaxError, whose text is not V8's.
func LoadGraph(root string) (*Graph, error) {
	p := IndexPath(root)
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no index at %s — run: weave index", p)
	}
	if err != nil {
		return nil, err
	}
	v, err := jsjson.Parse([]byte(jsstr.DecodeUTF8(raw)))
	if err != nil {
		return nil, err
	}
	// An index written by an older weave lacks fields the current code reads.
	if ver, _ := v.Object().Get("version"); ver.Kind() != jsjson.KindNumber || ver.Float() != GraphVersion {
		return nil, fmt.Errorf("index format %s is stale — rebuilding", templateString(ver, v.Object().Has("version")))
	}
	return DecodeGraph(v)
}

// templateString is `${v}` for the scalar values a version field can hold.
func templateString(v jsjson.Value, present bool) string {
	switch v.Kind() {
	case jsjson.KindNumber:
		return jsjson.FormatNumber(v.Float())
	case jsjson.KindString:
		return v.Str()
	case jsjson.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case jsjson.KindArray:
		return "[array]" // JS joins the elements; not reproduced
	case jsjson.KindObject:
		return "[object Object]"
	}
	if !present {
		return "undefined"
	}
	return "null"
}

// SaveGraph writes JSON.stringify(g) to the index and returns its path.
func SaveGraph(root string, g *Graph) (string, error) {
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		return "", err
	}
	p := IndexPath(root)
	return p, os.WriteFile(p, g.MarshalJS(), 0o644)
}

// LoadIfPresent is the previous index, or nil when it is missing, unreadable or
// of another version.
func LoadIfPresent(root string) *Graph {
	raw, err := os.ReadFile(IndexPath(root))
	if err != nil {
		return nil
	}
	v, err := jsjson.Parse([]byte(jsstr.DecodeUTF8(raw)))
	if err != nil {
		return nil
	}
	if ver, _ := v.Object().Get("version"); ver.Kind() != jsjson.KindNumber || ver.Float() != GraphVersion {
		return nil
	}
	g, err := DecodeGraph(v)
	if err != nil {
		return nil
	}
	return g
}

// IsFresh reports whether the index is newer than every source file, than the
// extractor code in codeDir (when given), and than .weave/patterns.json.
func IsFresh(root, codeDir string) bool {
	st, err := os.Stat(IndexPath(root))
	if err != nil {
		return false
	}
	stamp := mtimeMs(st)
	// A changed extractor invalidates every index it produced, not just changed sources.
	if codeDir != "" && NewestMtime(codeDir) > stamp {
		return false
	}
	// .weave/ is skipped by the walk, so a patterns file needs its own check.
	if pst, err := os.Stat(filepath.Join(root, ".weave", "patterns.json")); err == nil && mtimeMs(pst) > stamp {
		return false
	}
	return NewestMtime(root) <= stamp
}

// MarkSuspect flags every requirement whose text hash changed since prev, so the
// files and tests linked to it read as "suspect".
func MarkSuspect(g, prev *Graph) {
	before := map[string]string{}
	for _, n := range prev.Nodes {
		if n.Kind == KindReq {
			if h, ok := n.Meta.Get("hash"); ok && h.Kind() == jsjson.KindString {
				before[n.ID] = h.Str()
			}
		}
	}
	for _, n := range g.Nodes {
		if n.Kind != KindReq {
			continue
		}
		old := before[n.ID]
		now, ok := n.Meta.Get("hash")
		if old != "" && ok && now.Kind() == jsjson.KindString && old != now.Str() {
			n.Meta = n.Meta.Clone()
			n.Meta.Set("suspect", jsjson.Bool(true))
		}
	}
}
