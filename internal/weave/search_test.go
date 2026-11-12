package weave

import (
	"strings"
	"testing"
)

// An agent describes what it is after ("the inbox count"), not the name it is
// stored under (document.inbox.getCount). Search has to meet it there.
func TestSearchTakesPlainWords(t *testing.T) {
	g := &Graph{Nodes: []*GNode{
		{ID: "a", Kind: KindEndpoint, Name: "QUERY /api/trpc/document.inbox.getCount"},
		{ID: "b", Kind: KindEndpoint, Name: "QUERY /api/trpc/document.inbox.find"},
		{ID: "c", Kind: KindEndpoint, Name: "GET /tags"},
		{ID: "d", Kind: KindEndpoint, Name: "GET /articles/feed"},
		{ID: "e", Kind: KindEndpoint, Name: "POST /articles/:slug/favorite"},
	}}
	for q, want := range map[string]string{
		"inbox count":                 "document.inbox.getCount",
		"the inbox document count":    "document.inbox.getCount",
		"list all tags":               "GET /tags",
		"current user's article feed": "GET /articles/feed",
		"favorite an article":         "POST /articles/:slug/favorite",
	} {
		got := Search(g, q, 5)
		first := strings.SplitN(got, "\n", 2)[0]
		if !strings.Contains(first, want) {
			t.Errorf("Search(%q) first hit = %q, want %q", q, first, want)
		}
	}
	if got := Search(g, "inbox count", 5); strings.Contains(got, "inbox.find") {
		t.Errorf("a two-word query must match both words, got:\n%s", got)
	}
	if got := Search(g, "billing invoices", 5); !strings.HasPrefix(got, "nothing matched") {
		t.Errorf("unrelated words must not match, got:\n%s", got)
	}
}
