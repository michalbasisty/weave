package weave

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var reFunction = regexp.MustCompile(`function\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

// TestRenderExploreMatchesGolden locks the page to the HTML the TypeScript
// implementation produced before cutover, with the checkout root and build time
// normalised. It does not need Node.
func TestRenderExploreMatchesGolden(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "test", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	g := BuildGraph(root)
	g.Root = "__ROOT__"
	g.BuiltAt = "__ISO__"
	got := []byte(RenderExplore([]NamedGraph{{Name: "fixture", Graph: g}}))
	want, err := os.ReadFile(filepath.Join("testdata", "explore.golden.html"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		lo := max(0, i-120)
		hiA := min(len(got), i+120)
		hiB := min(len(want), i+120)
		t.Fatalf("explore differs at byte %d of %d:\n go: %q\nwant: %q", i, len(got), got[lo:hiA], want[lo:hiB])
	}

	// The page must carry the fixture's graph, not an undefined-filled one.
	if !bytes.Contains(got, []byte("page:/orders")) {
		t.Error("page:/orders missing from the payload")
	}
	if bytes.Contains(got, []byte("undefined")) {
		t.Error(`the page contains a literal "undefined"`)
	}
	seen := map[string]bool{}
	names := 0
	for _, m := range reFunction.FindAllSubmatch(got, -1) {
		name := string(m[1])
		names++
		if seen[name] {
			t.Errorf("function %q appears more than once in the inline script", name)
		}
		seen[name] = true
	}
	if names == 0 {
		t.Error("no function declarations found in the inline script")
	}
	t.Logf("%d bytes identical, %d named functions", len(got), names)
}

// The fixture has one change; the page promises newest first, which b.c.localeCompare(a.c)
// means. Lock the direction so a one-change fixture cannot hide it.
func TestExplorePayloadChangesNewestFirst(t *testing.T) {
	g := &Graph{
		Nodes: []*GNode{
			{ID: ChangeID("2026-01-01-a"), Kind: KindChange, Name: "2026-01-01-a"},
			{ID: TaskID("2026-01-01-a/T1"), Kind: KindTask, Name: "T1"},
			{ID: ChangeID("2026-02-02-b"), Kind: KindChange, Name: "2026-02-02-b"},
			{ID: TaskID("2026-02-02-b/T1"), Kind: KindTask, Name: "T1"},
		},
		Edges: []GEdge{
			{From: ChangeID("2026-01-01-a"), To: TaskID("2026-01-01-a/T1"), Kind: "has_task", Conf: ConfExact},
			{From: ChangeID("2026-02-02-b"), To: TaskID("2026-02-02-b/T1"), Kind: "has_task", Conf: ConfExact},
		},
	}
	p := ExplorePayload(g)
	if len(p.Changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(p.Changes))
	}
	if p.Changes[0].C != "change:2026-02-02-b" || p.Changes[1].C != "change:2026-01-01-a" {
		t.Fatalf("changes order = %q, %q; want newest first", p.Changes[0].C, p.Changes[1].C)
	}
}
