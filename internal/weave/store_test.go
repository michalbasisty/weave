package weave

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michalbasisty/weave/internal/jsjson"
)

var graphFiles = []string{"../../test/fixture", "../.."}

func readIndex(t *testing.T, root string) []byte {
	t.Helper()
	raw, err := os.ReadFile(IndexPath(root))
	if os.IsNotExist(err) {
		t.Skipf("%s missing: run `node src/cli.ts index` first", IndexPath(root))
	}
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Through the typed model, not just jsjson: load, save, same bytes.
func TestGraphRoundTripThroughModel(t *testing.T) {
	for _, root := range graphFiles {
		t.Run(root, func(t *testing.T) {
			raw := readIndex(t, root)
			g, err := LoadGraph(root)
			if err != nil {
				t.Fatal(err)
			}
			if g.Version != GraphVersion || len(g.Nodes) == 0 || len(g.Edges) == 0 || g.Coverage == nil || g.Perf == nil || len(g.Perf.Phases) == 0 {
				t.Fatalf("decoded graph is incomplete: %d nodes, %d edges", len(g.Nodes), len(g.Edges))
			}
			out := t.TempDir()
			p, err := SaveGraph(out, g)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(saved, raw) {
				i := 0
				for i < len(saved) && i < len(raw) && saved[i] == raw[i] {
					i++
				}
				t.Fatalf("saved graph differs at byte %d: %q vs %q", i, clip(saved, i), clip(raw, i))
			}
			t.Logf("%d nodes, %d edges, %d bytes identical", len(g.Nodes), len(g.Edges), len(raw))
		})
	}
}

func TestNodeKeyOrderSurvives(t *testing.T) {
	// `{ ...prev, ...n }` with prev lacking file puts meta before file.
	in := `{"version":3,"root":"r","builtAt":"b","fileCount":0,"nodes":[` +
		`{"id":"task:x","kind":"task","name":"x","meta":{"done":false,"title":"t"},"file":"","line":0,"extra":[1]},` +
		`{"id":"module:m","kind":"module","name":"m","file":""}],"edges":[],"prevEndpoints":[]}`
	v, err := jsjson.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	g, err := DecodeGraph(v)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(g.MarshalJS()); got != in {
		t.Fatalf("got  %s\nwant %s", got, in)
	}
}

func TestLoadGraphErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadGraph(root); err == nil || err.Error() != "no index at "+IndexPath(root)+" — run: weave index" {
		t.Fatalf("missing index: %v", err)
	}
	for body, want := range map[string]string{
		`{"version":2}`:   "index format 2 is stale — rebuilding",
		`{"root":"x"}`:    "index format undefined is stale — rebuilding",
		`{"version":"3"}`: "index format 3 is stale — rebuilding",
	} {
		if _, err := SaveGraph(root, &Graph{}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(IndexPath(root), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadGraph(root); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
		if LoadIfPresent(root) != nil {
			t.Errorf("%s: LoadIfPresent returned a stale graph", body)
		}
	}
}

func TestIsFresh(t *testing.T) {
	root := t.TempDir()
	copyTree(t, fixture, root)
	if IsFresh(root, "") {
		t.Fatal("fresh without an index")
	}
	g, err := LoadGraph(fixture)
	if err != nil {
		t.Skip(err)
	}
	p, err := SaveGraph(root, g)
	if err != nil {
		t.Fatal(err)
	}
	// Pin the index well after the copied sources, so the test does not depend on
	// filesystem timestamp resolution.
	stamp := time.Now().Add(time.Minute)
	touch(t, p, stamp)
	if !IsFresh(root, "") {
		t.Fatal("not fresh right after saving")
	}

	src := filepath.Join(root, "packages", "web", "src", "App.tsx")
	st, _ := os.Stat(src)
	touch(t, src, stamp.Add(time.Second))
	if IsFresh(root, "") {
		t.Fatal("still fresh after a source file changed")
	}
	touch(t, src, st.ModTime())
	if !IsFresh(root, "") {
		t.Fatal("not fresh after restoring the source mtime")
	}

	codeDir := t.TempDir()
	touch(t, filepath.Join(codeDir, "extract.ts"), stamp.Add(time.Second))
	if IsFresh(root, codeDir) {
		t.Fatal("still fresh after the extractor code changed")
	}

	pat := filepath.Join(root, ".weave", "patterns.json")
	touch(t, pat, stamp.Add(-time.Second))
	if !IsFresh(root, "") {
		t.Fatal("an older patterns.json made the index stale")
	}
	touch(t, pat, stamp.Add(time.Second))
	if IsFresh(root, "") {
		t.Fatal("still fresh after patterns.json changed")
	}
}

func TestMarkSuspect(t *testing.T) {
	req := func(id, hash string) *GNode {
		m := &jsjson.Object{}
		m.Set("title", jsjson.Str("t"))
		m.Set("desc", jsjson.Str("d"))
		m.Set("hash", jsjson.Str(hash))
		return &GNode{ID: "req:" + id, Kind: KindReq, Name: id, File: Ptr("spec.md"), Line: Ptr(3), Meta: m}
	}
	prev := &Graph{Nodes: []*GNode{req("A", "h1"), req("B", "h2"), req("C", ""), {ID: "file:x", Kind: KindFile, Name: "x"}}}
	g := &Graph{Nodes: []*GNode{req("A", "h1x"), req("B", "h2"), req("C", "c2"), req("D", "d"), {ID: "file:x", Kind: KindFile, Name: "x"}}}
	before := string(g.Nodes[0].AppendJS(nil))

	MarkSuspect(g, prev)

	want := `{"id":"req:A","kind":"req","name":"A","file":"spec.md","line":3,"meta":{"title":"t","desc":"d","hash":"h1x","suspect":true}}`
	if got := string(g.Nodes[0].AppendJS(nil)); got != want {
		t.Fatalf("changed req: %s\nwant %s (was %s)", got, want, before)
	}
	for _, n := range g.Nodes[1:] {
		if n.Meta.Has("suspect") {
			t.Errorf("%s marked suspect", n.ID)
		}
	}
	if prev.Nodes[0].Meta.Has("suspect") {
		t.Error("prev graph was modified")
	}
}

func touch(t *testing.T, p string, at time.Time) {
	t.Helper()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// copyTree copies the fixture's sources, not its .weave directory.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && rel != "." {
			return filepath.SkipDir
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func clip(b []byte, i int) string {
	return string(b[max(0, i-40):min(len(b), i+40)])
}
