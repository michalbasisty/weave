package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/delta"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/registry"
	"github.com/michalbasisty/weave/internal/weave"
)

func elapsed(g *weave.Graph) string {
	if g == nil || g.Perf == nil {
		return "0ms"
	}
	return fmt.Sprintf("%dms", int(math.Round(g.Perf.TotalMs)))
}

func joinParts(parts []string) string { return strings.Join(parts, "  ") }

func cmdExplore() int {
	all := hasFlag("all")
	out := arg("out", "")
	if out == "" {
		if all {
			out = filepath.Join(registry.HomeDir(), "explore.html")
		} else {
			out = filepath.Join(root, ".weave", "explore.html")
		}
	}
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	}
	var picked []weave.NamedGraph
	if all {
		entries := registry.Load()
		sort.Slice(entries, func(i, j int) bool {
			return jsstr.LocaleCompare(entries[i].LastUsed, entries[j].LastUsed) > 0
		})
		for _, e := range entries {
			var g *weave.Graph
			var err error
			if weave.IsFresh(e.Root, exeDir()) {
				g, err = weave.LoadGraph(e.Root)
			}
			if g == nil || err != nil {
				g, err = weave.Reindex(e.Root)
			}
			if err != nil || g == nil {
				continue
			}
			picked = append(picked, weave.NamedGraph{Name: e.Name, Graph: g})
		}
		if len(picked) == 0 {
			fmt.Fprintln(os.Stderr, "no indexed projects yet — run `weave` inside a repo first")
			return 0
		}
	} else {
		g, code := graphFor()
		if g == nil {
			return code
		}
		picked = append(picked, weave.NamedGraph{Name: filepath.Base(root), Graph: g})
	}
	// "Since the last commit" is the first thing a person checking an agent's work
	// wants. A repo without git (or --no-delta) simply has no such section.
	if !hasFlag("no-delta") {
		since := arg("since", "HEAD")
		for i := range picked {
			if base := baseline.GraphAt(picked[i].Graph.Root, since); base != nil {
				picked[i].Delta = delta.Explore(picked[i].Graph, base, since)
			}
		}
	}
	html := weave.RenderExplore(picked)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(out, []byte(html), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	kb := int(math.Round(float64(jsstr.Len(html)) / 1024))
	fmt.Printf("explore: %s (%d KB, %d project(s))\n", out, kb, len(picked))
	if !hasFlag("no-open") {
		openPath(out)
	}
	return 0
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return filepath.Dir(resolved)
	}
	return filepath.Dir(exe)
}

func openPath(p string) {
	var c *exec.Cmd
	switch {
	case os.PathSeparator == '\\':
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", p)
	case isDarwin():
		c = exec.Command("open", p)
	default:
		c = exec.Command("xdg-open", p)
	}
	_ = c.Start()
}

func isDarwin() bool {
	_, err := os.Stat("/System/Library")
	return err == nil
}
