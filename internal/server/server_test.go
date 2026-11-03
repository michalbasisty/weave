package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

func testHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &weave.Graph{Version: weave.GraphVersion, Root: root, FileCount: 1, Nodes: []*weave.GNode{{ID: "file:a.ts", Kind: weave.KindFile, Name: "a.ts"}}}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return g, nil }})
	return h, root
}

// The page must carry the action layer and the write token, or none of the buttons
// in it can work.
func TestPageCarriesTheActionLayer(t *testing.T) {
	h, _ := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"/app.js", "/app.css", `window.__weave={token:"tok"}`} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

// A write without the token is refused: the page has the token, a forged request does not.
func TestWritesNeedTheToken(t *testing.T) {
	h, _ := testHandler(t)
	for _, path := range []string{"/api/check", "/api/note", "/api/wiki", "/api/task-done"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without a token: got %d, want 403", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/note", strings.NewReader(`{"note":"x"}`))
	req.Header.Set("X-Weave-Token", "tok")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("note without files should be a 400, got %d", rec.Code)
	}
}

// A browser on another origin cannot drive the local server.
func TestCrossOriginRefused(t *testing.T) {
	h, _ := testHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin got %d, want 403", rec.Code)
	}
}

// /api/file answers one window of a file, refuses paths outside the repo and dotfiles.
func TestFileWindowAndGuards(t *testing.T) {
	h, root := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/file?path=a.ts", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Path, Text string
		Total      int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Text, "export const a = 1") || got.Total != 2 {
		t.Errorf("unexpected window: %+v", got)
	}

	for _, bad := range []string{"../outside.ts", ".env", "node_modules/x.ts"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/file?path="+bad, nil))
		if rec.Code == 200 {
			t.Errorf("%s should not be served", bad)
		}
	}
	_ = root
}

func TestStatusReportsCounts(t *testing.T) {
	h, root := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Root   string
		Counts map[string]int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Root != root || got.Counts[weave.KindFile] != 1 {
		t.Errorf("unexpected status: %+v", got)
	}
}

func TestTokenIsRandomPerRun(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) != 32 || a == b {
		t.Fatalf("tokens: %q %q", a, b)
	}
}
