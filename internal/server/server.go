// Package server serves the explorer and the wiki from one process, and lets the
// page do the things a person actually needs: run the gate, open the file behind a
// failure, leave a note, close a task, regenerate the wiki.
//
// It binds loopback only and refuses cross-origin writes. It is not a team server and
// must not become one: the product's claim is a local gate, not a hosted dashboard.
package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/delta"
	"github.com/michalbasisty/weave/internal/weave"
)

//go:embed assets/app.js assets/app.css
var assets embed.FS

// Config is everything the handler needs. Graph is called per request so a stale
// index is rebuilt before the page is drawn, the same rule the MCP server follows.
type Config struct {
	Root  string
	Graph func() (*weave.Graph, error)
	Token string
}

// NewToken is the per-run write token embedded in the page.
func NewToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "weave"
	}
	return hex.EncodeToString(b[:])
}

func (c Config) bin() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "weave"
}

// run executes a weave subcommand and returns its combined output.
func (c Config) run(args ...string) (string, int) {
	cmd := exec.Command(c.bin(), args...)
	cmd.Dir = c.Root
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return err.Error(), 1
		}
	}
	return string(out), code
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// localOnly rejects a request a browser on another origin could have forged, and a
// Host header that is not loopback (DNS rebinding).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			if !strings.HasPrefix(o, "http://127.0.0.1") && !strings.HasPrefix(o, "http://localhost") {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		if h := r.Host; h != "" {
			name := h
			if i := strings.LastIndexByte(name, ':'); i > 0 {
				name = name[:i]
			}
			if name != "127.0.0.1" && name != "localhost" && name != "[::1]" {
				http.Error(w, "host refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (c Config) authed(r *http.Request) bool {
	if c.Token == "" {
		return true
	}
	return r.Header.Get("X-Weave-Token") == c.Token || r.URL.Query().Get("t") == c.Token
}

// Handler wires every route. One panel: `/` is the app, `/wiki/` is the generated
// documentation, `/api/*` is what the page can do.
func Handler(c Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		g, err := c.Graph()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ng := weave.NamedGraph{Name: filepath.Base(c.Root), Graph: g}
		// "Since the last commit" is the first thing a person checking an agent's work
		// wants, so the hosted page computes it the same way `weave explore` does.
		if base := baseline.GraphAt(c.Root, "HEAD"); base != nil {
			ng.Delta = delta.Explore(g, base, "HEAD")
		}
		page := weave.RenderExplore([]weave.NamedGraph{ng})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, inject(page, c.Token))
	})

	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("assets/app.js")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/app.css", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("assets/app.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(b)
	})

	wikiDir := filepath.Join(c.Root, ".weave", "wiki")
	mux.Handle("/wiki/", http.StripPrefix("/wiki/", http.FileServer(http.Dir(wikiDir))))

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		counts := map[string]int{}
		for _, n := range g.Nodes {
			counts[n.Kind]++
		}
		writeJSON(w, 200, map[string]any{"root": c.Root, "builtAt": g.BuiltAt, "files": g.FileCount, "counts": counts})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"name": filepath.Base(c.Root), "graph": json.RawMessage(g.MarshalJS())})
	})

	mux.HandleFunc("/api/check", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		out, code := c.run("check", "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	mux.HandleFunc("/api/reindex", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		out, code := c.run("index", "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	mux.HandleFunc("/api/wiki", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		out, code := c.run("wiki", "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out, "url": "/wiki/"})
	})

	// remember writes a note beside the files it concerns.
	mux.HandleFunc("/api/note", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		var body struct {
			Note  string   `json:"note"`
			Files []string `json:"files"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.Note) == "" || len(body.Files) == 0 {
			writeJSON(w, 400, map[string]string{"error": "note and files are required"})
			return
		}
		args := append([]string{"remember", "--note", body.Note, "--root", c.Root}, body.Files...)
		out, code := c.run(args...)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	// task-done closes one task in a change record, through the same command the CLI uses.
	mux.HandleFunc("/api/task-done", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		var body struct{ Change, Task string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Change == "" || body.Task == "" {
			writeJSON(w, 400, map[string]string{"error": "change and task are required"})
			return
		}
		out, code := c.run("spec", "task", "done", body.Change, body.Task, "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	// file opens one window of a file, never the whole file.
	mux.HandleFunc("/api/review", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, buildReview(g, c.Root))
	})

	// review/mark closes the range: everything before this commit counts as seen.
	mux.HandleFunc("/api/review/mark", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		head := gitOut(c.Root, "rev-parse", "HEAD")
		if head == "" {
			writeJSON(w, 400, map[string]string{"error": "no git history to mark"})
			return
		}
		if err := appendMark(c.Root, head); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"marked": head})
	})
	mux.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		line, _ := strconv.Atoi(r.URL.Query().Get("line"))
		if rel == "" {
			writeJSON(w, 400, map[string]string{"error": "path is required"})
			return
		}
		full := filepath.Join(c.Root, filepath.FromSlash(rel))
		abs, err := filepath.Abs(full)
		rootAbs, _ := filepath.Abs(c.Root)
		if err != nil || !strings.HasPrefix(abs, rootAbs) {
			writeJSON(w, 400, map[string]string{"error": "outside the repo"})
			return
		}
		if strings.Contains(rel, "node_modules/") || strings.HasPrefix(filepath.Base(rel), ".") {
			writeJSON(w, 400, map[string]string{"error": "not served"})
			return
		}
		info, err := os.Stat(abs)
		if err != nil || info.Size() > 512*1024 {
			writeJSON(w, 400, map[string]string{"error": "unreadable or larger than 512 KB"})
			return
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		start, end := 0, len(lines)
		if line > 0 {
			start = line - 26
			if start < 0 {
				start = 0
			}
			end = line + 44
			if end > len(lines) {
				end = len(lines)
			}
		} else if end > 200 {
			end = 200
		}
		numbered := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			numbered = append(numbered, fmt.Sprintf("%5d  %s", i+1, lines[i]))
		}
		writeJSON(w, 200, map[string]any{"path": rel, "start": start + 1, "text": strings.Join(numbered, "\n"), "total": len(lines)})
	})

	return localOnly(mux)
}

// inject puts the action layer into the rendered page: a stylesheet, the script, and
// the write token the script uses.
func inject(page, token string) string {
	layer := `<link rel="stylesheet" href="/app.css">
<script>window.__weave={token:"` + token + `"}</script>
<script src="/app.js"></script>
`
	if i := strings.LastIndex(page, "</body>"); i >= 0 {
		return page[:i] + layer + page[i:]
	}
	return page + layer
}
