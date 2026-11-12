package weave

import "testing"

func TestClosestNamesTheLikelyRoute(t *testing.T) {
	g := &Graph{Nodes: []*GNode{
		{ID: "a", Kind: KindEndpoint, Name: "GET /tag-list"},
		{ID: "b", Kind: KindEndpoint, Name: "GET /user"},
		{ID: "c", Kind: KindEndpoint, Name: "PUT /tasks/:id/stop"},
		{ID: "d", Kind: KindEndpoint, Name: "GET /articles/:slug"},
	}}
	for call, want := range map[string]string{
		"GET /tags":               "GET /tag-list",       // renamed, stem kept
		"PUT /tasks/${id}/pause":  "PUT /tasks/:id/stop", // one segment off
		"GET /article/${slug}":    "GET /articles/:slug", // a typo
		"GET /billing/invoices":   "",                    // nothing near: no guess
		"DELETE /totally/unknown": "",
	} {
		if got := Closest(g, call); got != want {
			t.Errorf("Closest(%q) = %q, want %q", call, got, want)
		}
	}
}
