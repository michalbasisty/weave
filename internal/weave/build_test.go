package weave

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/michalbasisty/weave/internal/jsjson"
)

// copyFixture copies test/fixture into a temp dir, without its index.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "test", "fixture")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && d.Name() == ".weave" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// comparable parses a graph and drops what differs between runs: the timestamp,
// the timings and the absolute root the index was built in.
func comparable(t *testing.T, raw []byte) []byte {
	t.Helper()
	v, err := jsjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	o := v.Object()
	o.Delete("builtAt")
	o.Delete("perf")
	o.Set("root", jsjson.Str("<root>"))
	return jsjson.Marshal(v)
}

// The fixture graph built by Go equals the one the TypeScript built
// (testdata/fixture.graph.json, from `node src/cli.ts index --root <copy of test/fixture>`).
func TestFixtureGraphMatchesTypeScript(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "fixture.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := copyFixture(t)
	g := BuildGraph(root)
	if g.Root != root || g.Perf == nil || len(g.Perf.Phases) != 5 {
		t.Fatalf("root %q, perf %+v", g.Root, g.Perf)
	}
	want := comparable(t, golden)
	got := comparable(t, g.MarshalJS())
	if !bytes.Equal(got, want) {
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		t.Fatalf("graph differs at byte %d:\n go: %q\n ts: %q", i, clip(got, i), clip(want, i))
	}
	t.Logf("%d nodes, %d edges, %d bytes identical", len(g.Nodes), len(g.Edges), len(got))
}

// Reindex keeps the previous endpoints and writes the index the store reads back.
func TestReindexRoundTrip(t *testing.T) {
	t.Setenv("WEAVE_HOME", t.TempDir())
	root := copyFixture(t)
	first, err := Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.PrevEndpoints != nil {
		t.Fatalf("first run has prevEndpoints %v", first.PrevEndpoints)
	}
	second, err := Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.PrevEndpoints) != 9 {
		t.Fatalf("prevEndpoints = %v", second.PrevEndpoints)
	}
	loaded, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.MarshalJS(), second.MarshalJS()) {
		t.Fatal("saved index does not read back identically")
	}
	perf, err := os.ReadFile(filepath.Join(root, ".weave", "perf.jsonl"))
	if err != nil || bytes.Count(perf, []byte("\n")) != 2 {
		t.Fatalf("perf.jsonl: %q, %v", perf, err)
	}
}
