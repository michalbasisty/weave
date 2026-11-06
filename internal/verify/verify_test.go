package verify

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

// A commit that removed `users/:id` was reported as breaking
// `api.get('/reports${params}')`: the endpoint's parameter matched an unrelated
// single-segment path. A match needs a literal segment in common.
func TestSameShapeNeedsALiteralSegment(t *testing.T) {
	cases := []struct {
		call, endpoint string
		want           bool
	}{
		{"GET /reports", "GET /api/v1/users/:id", false},
		{"GET /stats", "GET /api/v1/users/:id", false},
		{"GET /tasks/:id", "GET /api/v1/users/:id", false},
		{"GET /tasks/42", "GET /api/v1/tasks/:id", true},
		{"GET /api/v1/tasks/42", "GET /api/v1/tasks/:id", true},
		{"GET /api/v1/tasks", "GET /api/v1/tasks", true},
		{"DELETE /api/v1/tasks/:id", "GET /api/v1/tasks/:id", false},
	}
	for _, c := range cases {
		if got := sameShape(c.endpoint, c.call); got != c.want {
			t.Errorf("sameShape(%q, %q) = %v, want %v", c.endpoint, c.call, got, c.want)
		}
	}
}

// An endpoint that is gone but still called is the gate's one hard failure.
func TestRemovedEndpointStillCalledFails(t *testing.T) {
	root := t.TempDir()
	file, line := "packages/web/src/orders.tsx", 12
	g := &weave.Graph{
		Root: root,
		Nodes: []*weave.GNode{
			{ID: "call1", Kind: weave.KindCall, Name: "GET /api/v1/users/42", File: &file, Line: &line},
		},
		PrevEndpoints: []string{"GET /api/v1/users/:id"},
	}
	res := Run(g, root, "", false, nil)

	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "removed endpoint still called: GET /api/v1/users/:id from packages/web/src/orders.tsx:12") {
		t.Fatalf("errors = %v", res.Errors)
	}
	text := res.Text()
	if !strings.HasPrefix(text, "check: 0 endpoint(s) indexed, no changed file named, 1 endpoint(s) removed\n  FAIL ") {
		t.Fatalf("text:\n%s", text)
	}
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("a single failure is one line after the scope:\n%s", text)
	}
}

// A diff the gate has nothing to say about still returns the commands that prove it.
func TestCleanDiffHasNoErrorsAndCommands(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	res := Run(g, root, "", false, []string{"packages/backend/src/modules/orders/orders.routes.ts"})

	if len(res.Errors) != 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	if len(res.Commands) == 0 {
		t.Fatalf("no commands: %+v", res)
	}
	text := res.Text()
	if !strings.HasPrefix(text, "check: 1 file(s) → 2 endpoint(s)") {
		t.Fatalf("scope:\n%s", text)
	}
	if !strings.Contains(text, "\n  run  ") {
		t.Fatalf("commands missing from the text:\n%s", text)
	}
}

// strict is the only switch: the same gap is a warning or a failure.
func TestStrictTurnsAnUntestedEndpointIntoAFailure(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	files := []string{"packages/backend/src/modules/orders/orders.routes.ts"}

	lenient := Run(g, root, "", false, files)
	strict := Run(g, root, "", true, files)
	if len(lenient.Errors) != 0 {
		t.Fatalf("lenient errors = %v", lenient.Errors)
	}
	if len(strict.Errors) == 0 || !strings.Contains(strict.Text(), "FAIL ") {
		t.Fatalf("strict did not fail:\n%s", strict.Text())
	}
}

// The PR comment names the removed endpoint, its caller and the screens that reach it.
func TestMarkdownNamesScreensOfABrokenCall(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	// The backend dropped GET /api/v1/orders; Orders.tsx still calls it.
	gone, file, line := "GET /api/v1/orders", "packages/web/src/pages/Orders.tsx", 9
	kept := g.Nodes[:0]
	for _, n := range g.Nodes {
		if !(n.Kind == weave.KindEndpoint && n.Name == gone) {
			kept = append(kept, n)
		}
	}
	g.Nodes = append(kept, &weave.GNode{ID: weave.CallID("GET", "/api/v1/orders"), Kind: weave.KindCall, Name: gone, File: &file, Line: &line})
	g.PrevEndpoints = []string{gone}

	res := Run(g, root, "", false, []string{"README.md"})
	if len(res.Broken) == 0 {
		t.Fatalf("nothing broken: %+v", res)
	}
	md := res.Markdown()
	if !strings.HasPrefix(md, Marker+"\n### ❌ weave check") || !strings.Contains(md, "`"+gone+"`") {
		t.Fatalf("markdown:\n%s", md)
	}
	if len(res.Broken[0].Screens) == 0 || !strings.Contains(md, "`"+res.Broken[0].Screens[0]+"`") {
		t.Fatalf("no screens for %s:\n%s", gone, md)
	}
	t.Log("\n" + md)
}
