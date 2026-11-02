package weave

import (
	"github.com/michalbasisty/weave/internal/perf"
	"github.com/michalbasisty/weave/internal/registry"
)

// Reindex rebuilds the graph, marks requirements whose text changed since the
// previous index, remembers the previous endpoints, saves, and records timings.
func Reindex(root string) (*Graph, error) {
	prev := LoadIfPresent(root)
	g := BuildGraph(root)
	if prev != nil {
		MarkSuspect(g, prev)
		g.PrevEndpoints = []string{}
		for _, n := range prev.Nodes {
			if n.Kind == KindEndpoint {
				g.PrevEndpoints = append(g.PrevEndpoints, n.Name)
			}
		}
	}
	if _, err := SaveGraph(root, g); err != nil {
		return nil, err
	}
	NoteCounts(root, g)
	if g.Perf != nil {
		rec := perf.Record{
			At:      g.BuiltAt,
			Files:   float64(g.FileCount),
			Nodes:   float64(len(g.Nodes)),
			Edges:   float64(len(g.Edges)),
			TotalMs: g.Perf.TotalMs,
			Phases:  map[string]float64{},
			HeapMb:  g.Perf.HeapMb,
		}
		for _, ph := range g.Perf.Phases {
			rec.Phases[ph.Name] = ph.Ms
			rec.PhaseOrder = append(rec.PhaseOrder, ph.Name)
		}
		perf.Append(root, rec)
	}
	return g, nil
}

// NoteCounts keeps the project list's numbers current, even when the index was
// already warm.
func NoteCounts(root string, g *Graph) {
	c := registry.Counts{Files: g.FileCount}
	for _, n := range g.Nodes {
		switch n.Kind {
		case KindEndpoint:
			c.Endpoints++
		case KindChange:
			c.Changes++
		}
	}
	registry.NoteIndex(root, c)
}
