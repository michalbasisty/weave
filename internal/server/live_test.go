package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

// The live page is served, and the map it draws has the app's three columns.
func TestLivePageAndMap(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	h := Handler(Config{Root: root, Graph: func() (*weave.Graph, error) { return g, nil }})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/live"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "weave live · fixture") || strings.Contains(rec.Body.String(), "{{name}}") {
		t.Fatalf("live page: %d\n%.300s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/live/map"))
	var m liveMap
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Screens) == 0 || len(m.Endpoints) == 0 || len(m.Calls) == 0 {
		t.Fatalf("map: %+v", m)
	}
	known := map[string]bool{}
	for _, e := range m.Endpoints {
		known[e.Name] = true
	}
	for _, c := range m.Calls {
		if !known[c[1]] {
			t.Fatalf("call to an endpoint not on the map: %v", c)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/live"))
	var st liveState
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Print == "" || st.Blast.Broken == nil || st.Changes == nil {
		t.Fatalf("state: %+v", st)
	}
}
