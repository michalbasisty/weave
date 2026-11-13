package server

import (
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/michalbasisty/weave/internal/verify"
	"github.com/michalbasisty/weave/internal/weave"
)

// The live map: /live draws the whole app (screens → endpoints → tables) and
// lights up what the working tree's change reaches, while you or an agent edit.
// /api/live is a long poll that answers as soon as the tree moves.

//go:embed assets/live.html
var liveHTML string

// liveMap is the whole app, drawn once per index.
type liveMap struct {
	Name      string      `json:"name"`
	BuiltAt   string      `json:"builtAt"`
	Screens   []liveNode  `json:"screens"`
	Endpoints []liveNode  `json:"endpoints"`
	Tables    []liveNode  `json:"tables"`
	Calls     [][2]string `json:"calls"` // screen name, endpoint name
	Uses      [][2]string `json:"uses"`  // endpoint name, table name
}

type liveNode struct {
	Name   string `json:"name"`
	File   string `json:"file,omitempty"`
	Module string `json:"module,omitempty"`
}

func buildLiveMap(name string, g *weave.Graph) liveMap {
	m := liveMap{Name: name, BuiltAt: g.BuiltAt, Screens: []liveNode{}, Endpoints: []liveNode{}, Tables: []liveNode{}, Calls: [][2]string{}, Uses: [][2]string{}}
	byID := make(map[string]*weave.GNode, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	moduleOf := map[string]string{}
	modules := map[string][]string{} // module id -> endpoint ids
	models := map[string][]string{}  // module id -> model ids
	tables := map[string][]string{}  // model id -> table ids
	for _, e := range g.Edges {
		switch e.Kind {
		case "in_module":
			moduleOf[e.From] = e.To
			modules[e.To] = append(modules[e.To], e.From)
		case "uses":
			models[e.From] = append(models[e.From], e.To)
		case "maps_to":
			tables[e.From] = append(tables[e.From], e.To)
		}
	}
	file := func(n *weave.GNode) string {
		if n.File != nil {
			return *n.File
		}
		return ""
	}
	for _, n := range g.Nodes {
		switch n.Kind {
		case weave.KindPage:
			m.Screens = append(m.Screens, liveNode{Name: n.Name, File: file(n)})
		case weave.KindEndpoint:
			mod := ""
			if mn := byID[moduleOf[n.ID]]; mn != nil {
				mod = mn.Name
			}
			m.Endpoints = append(m.Endpoints, liveNode{Name: n.Name, File: file(n), Module: mod})
		case "table":
			m.Tables = append(m.Tables, liveNode{Name: n.Name, File: file(n)})
		}
	}
	sort.Slice(m.Screens, func(i, j int) bool { return m.Screens[i].Name < m.Screens[j].Name })
	sort.Slice(m.Endpoints, func(i, j int) bool {
		a, b := m.Endpoints[i], m.Endpoints[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Name < b.Name
	})
	sort.Slice(m.Tables, func(i, j int) bool { return m.Tables[i].Name < m.Tables[j].Name })
	for _, e := range g.Edges {
		if e.Kind == "calls" && strings.HasPrefix(e.From, "page:") && strings.HasPrefix(e.To, "endpoint:") {
			if a, b := byID[e.From], byID[e.To]; a != nil && b != nil {
				m.Calls = append(m.Calls, [2]string{a.Name, b.Name})
			}
		}
	}
	seen := map[[2]string]bool{}
	for mod, eps := range modules {
		for _, model := range models[mod] {
			for _, t := range tables[model] {
				tn := byID[t]
				if tn == nil {
					continue
				}
				for _, ep := range eps {
					if en := byID[ep]; en != nil && !seen[[2]string{en.Name, tn.Name}] {
						seen[[2]string{en.Name, tn.Name}] = true
						m.Uses = append(m.Uses, [2]string{en.Name, tn.Name})
					}
				}
			}
		}
	}
	return m
}

// treePrint changes whenever the working tree does: HEAD, the uncommitted file
// list, and each listed file's size and modification time.
func treePrint(root string) string {
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, _ := cmd.Output()
		return string(out)
	}
	h := sha1.New()
	h.Write([]byte(git("rev-parse", "HEAD")))
	status := git("status", "--porcelain=v1", "--untracked-files=all")
	h.Write([]byte(status))
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		f := line[3:]
		if i := strings.Index(f, " -> "); i >= 0 {
			f = f[i+4:]
		}
		f = strings.Trim(f, `"`)
		if strings.HasPrefix(f, ".weave/") {
			continue
		}
		if st, err := os.Stat(filepath.Join(root, f)); err == nil {
			fmt.Fprintf(h, "%s %d %d\n", f, st.Size(), st.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// liveState is what the change reaches right now.
type liveState struct {
	Print    string       `json:"print"`
	At       string       `json:"at"`
	BuiltAt  string       `json:"builtAt"`
	OK       bool         `json:"ok"`
	Scope    string       `json:"scope"`
	Errors   []string     `json:"errors"`
	Changes  []string     `json:"changes"`
	Commands []string     `json:"commands"`
	Blast    verify.Blast `json:"blast"`
	Error    string       `json:"error,omitempty"`
}

type liveCache struct {
	mu    sync.Mutex
	print string
	state liveState
}

func (c Config) liveNow(cache *liveCache) liveState {
	print := treePrint(c.Root)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.print == print && cache.state.Print != "" {
		return cache.state
	}
	st := liveState{Print: print, At: time.Now().Format(time.RFC3339), Errors: []string{}, Changes: []string{}, Commands: []string{}}
	g, err := c.Graph()
	if err != nil {
		st.Error = err.Error()
		return st
	}
	res := verify.Run(g, c.Root, "", false, nil)
	st.BuiltAt = g.BuiltAt
	st.OK = len(res.Errors) == 0
	st.Scope = res.Scope
	st.Blast = res.Blast
	if res.Errors != nil {
		st.Errors = res.Errors
	}
	if res.Changes != nil {
		st.Changes = res.Changes
	}
	if res.Commands != nil {
		st.Commands = res.Commands
	}
	cache.print, cache.state = print, st
	return st
}

func (c Config) liveRoutes(mux *http.ServeMux) {
	cache := &liveCache{}
	mux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.Replace(liveHTML, "{{name}}", htmlEscape(filepath.Base(c.Root)), -1)))
	})
	mux.HandleFunc("/api/live/map", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, buildLiveMap(filepath.Base(c.Root), g))
	})
	// A long poll: answer at once when the tree differs from `since`, else wait
	// for it to move, at most 20 seconds.
	mux.HandleFunc("/api/live", func(w http.ResponseWriter, r *http.Request) {
		since := r.URL.Query().Get("since")
		deadline := time.Now().Add(20 * time.Second)
		for {
			if since == "" || treePrint(c.Root) != since || time.Now().After(deadline) {
				writeJSON(w, 200, c.liveNow(cache))
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(700 * time.Millisecond):
			}
		}
	})
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
