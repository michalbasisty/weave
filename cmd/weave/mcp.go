package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/mcp"
	"github.com/michalbasisty/weave/internal/registry"
	"github.com/michalbasisty/weave/internal/weave"
)

// The MCP server keeps one graph per project and rebuilds only when the checkout or
// this binary changed. A burst of calls skips the stat sweep for 1.5 seconds.
func cmdMcp() int {
	multi := hasFlag("all")
	manager := hasFlag("manager")
	mcp.FullToolset = hasFlag("all-tools")
	graphs := map[string]*weave.Graph{}
	checked := map[string]time.Time{}

	src := func(project string) (*weave.Graph, error) {
		r := root
		if project != "" {
			hit := registry.Resolve(project)
			if hit == nil {
				names := make([]string, 0)
				for _, e := range registry.Load() {
					names = append(names, e.Name)
				}
				known := "none"
				if len(names) > 0 {
					known = strings.Join(names, ", ")
				}
				return nil, fmt.Errorf("unknown project %q. Known: %s", project, known)
			}
			r = hit.Root
		}
		if g, ok := graphs[r]; ok && time.Since(checked[r]) < 1500*time.Millisecond {
			return g, nil
		}
		checked[r] = time.Now()
		if g, ok := graphs[r]; ok && weave.IsFresh(r, exeDir()) {
			return g, nil
		}
		g, err := weave.Reindex(r)
		if err != nil {
			return nil, err
		}
		graphs[r] = g
		return g, nil
	}

	if err := mcp.Serve(src, multi, manager, func() {
		// A spec write makes every cached graph stale, including a project not asked for.
		for k := range graphs {
			delete(graphs, k)
		}
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
