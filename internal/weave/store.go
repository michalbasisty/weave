package weave

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/rootio"
)

// IndexPath is <root>/.weave/graph.json.
func IndexPath(root string) string {
	return filepath.Join(root, ".weave", "graph.json")
}

// writableRoot pins the repository root, creating the root directory when it does
// not exist. Every write below then resolves inside that one handle, so a .weave
// directory or file swapped for a link out of the repo cannot redirect it. A link
// that stays inside the repo is refused the same way; weaveDirErr below turns that
// refusal into the directory to fix.
//
// The directory creation matches the old os.MkdirAll(root/.weave): SaveGraph and
// Remember still create a missing root. Parent .weave directories are created
// separately, still inside the pinned handle.
func writableRoot(root string) (*os.Root, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(abs)
}

// readWeaveFileAt reads name (a .weave path) through a pinned root. rootio refuses
// a FIFO or other irregular handle without waiting on it, and the handle serves the
// whole read, so a name swapped after the open cannot change the bytes. There is no
// size limit: an index or memory file is read whole.
func readWeaveFileAt(r *os.Root, name string) ([]byte, error) {
	f, err := rootio.OpenRegular(r, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// readWeaveFile pins root for one read and returns the file's bytes.
func readWeaveFile(root, name string) ([]byte, error) {
	r, err := openRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readWeaveFileAt(r, name)
}

// weaveDirErr adds the repair to os.Root's terse refusal of the default .weave
// directory: a junction or an absolute symlink named there, or standing in for
// graph.json, is not followed even when it points inside the repo. The error names
// the path to replace, and only the message changes: the refusal stands and nothing
// falls back to a path outside the pinned root.
func weaveDirErr(root, name string, err error) error {
	return rootio.ExplainPathEscapes(filepath.Join(root, name), err)
}

// LoadGraph reads the index, refusing one written by another graph version.
// A malformed file fails with a jsjson.SyntaxError, whose text is not V8's.
func LoadGraph(root string) (*Graph, error) {
	p := IndexPath(root)
	raw, err := readWeaveFile(root, ".weave/graph.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no index at %s — run: weave index", p)
	}
	if err != nil {
		return nil, weaveDirErr(root, ".weave/graph.json", err)
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
	r, err := writableRoot(root)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if err := r.MkdirAll(".weave", 0o755); err != nil {
		return "", weaveDirErr(root, ".weave", err)
	}
	p := IndexPath(root)
	if err := rootio.WriteFile(r, ".weave/graph.json", g.MarshalJS(), 0o644); err != nil {
		return "", weaveDirErr(root, ".weave/graph.json", err)
	}
	return p, nil
}

// LoadIfPresent is the previous index, or nil when it is missing, unreadable or
// of another version.
func LoadIfPresent(root string) *Graph {
	raw, err := readWeaveFile(root, ".weave/graph.json")
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
	r, err := openRoot(root)
	if err != nil {
		return false
	}
	defer r.Close()
	st, err := r.Stat(".weave/graph.json")
	if err != nil {
		return false
	}
	stamp := mtimeMs(st)
	// A changed extractor invalidates every index it produced, not just changed sources.
	if codeDir != "" && NewestMtime(codeDir) > stamp {
		return false
	}
	// .weave/ is skipped by the walk, so a patterns file needs its own check.
	if pst, err := r.Stat(".weave/patterns.json"); err == nil && mtimeMs(pst) > stamp {
		return false
	}
	// Strictly older: a file written in the same millisecond as the index may have
	// been written after it, and a needless rebuild is cheaper than a stale answer.
	return NewestMtime(root) < stamp
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
