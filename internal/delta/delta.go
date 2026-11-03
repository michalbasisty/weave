// Package delta is the Go port of src/delta.ts: what the graph gained or lost.
// Names and files, never a diff of code.
package delta

import (
	"sort"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/weave"
)

// kinds are the node kinds a delta reports, in output order.
var kinds = []string{
	weave.KindEndpoint, weave.KindPage, weave.KindTable,
	weave.KindModule, weave.KindChange, weave.KindReq,
}

var kindOrder = func() map[string]int {
	m := make(map[string]int, len(kinds))
	for i, k := range kinds {
		m[k] = i
	}
	return m
}()

type index struct {
	byID  map[string]*weave.GNode
	order []*weave.GNode
}

// newIndex keeps the first insertion position of an id, as a JavaScript Map does.
func newIndex(g *weave.Graph) *index {
	ix := &index{byID: make(map[string]*weave.GNode, len(g.Nodes))}
	for _, n := range g.Nodes {
		if _, ok := kindOrder[n.Kind]; !ok {
			continue
		}
		if _, seen := ix.byID[n.ID]; !seen {
			ix.order = append(ix.order, n)
		}
		ix.byID[n.ID] = n
	}
	return ix
}

// diffs is what the graph gained, lost and moved between base and current, each
// list in report order, plus the file call sites base had for every endpoint.
type diffs struct {
	added, removed, moved []*weave.GNode
	before                *index
	callers               map[string]int
}

func diff(current, base *weave.Graph) diffs {
	before := newIndex(base)
	after := newIndex(current)

	order := func(a, b *weave.GNode) bool {
		if x, y := kindOrder[a.Kind], kindOrder[b.Kind]; x != y {
			return x < y
		}
		return jsstr.LocaleCompare(a.Name, b.Name) < 0
	}

	var added, removed, moved []*weave.GNode
	for _, n := range after.order {
		if _, ok := before.byID[n.ID]; !ok {
			added = append(added, n)
		} else if !sameFile(before.byID[n.ID].File, n.File) {
			moved = append(moved, n)
		}
	}
	for _, n := range before.order {
		if _, ok := after.byID[n.ID]; !ok {
			removed = append(removed, n)
		}
	}
	sort.SliceStable(added, func(i, j int) bool { return order(added[i], added[j]) })
	sort.SliceStable(removed, func(i, j int) bool { return order(removed[i], removed[j]) })
	sort.SliceStable(moved, func(i, j int) bool { return order(moved[i], moved[j]) })

	// A removed endpoint the old graph still had callers for is the line a reader
	// must not miss. Count file call sites only: page edges are inferred from the
	// same call and would double it.
	kindByID := make(map[string]string, len(base.Nodes))
	for _, n := range base.Nodes {
		kindByID[n.ID] = n.Kind
	}
	callers := map[string]int{}
	for _, e := range base.Edges {
		if e.Kind != "calls" || kindByID[e.From] != weave.KindFile {
			continue
		}
		callers[e.To]++
	}
	return diffs{added: added, removed: removed, moved: moved, before: before, callers: callers}
}

// Explore is the delta the explorer page shows under "Since the last commit".
func Explore(current, base *weave.Graph, ref string) *weave.ExploreDelta {
	d := diff(current, base)
	item := func(n *weave.GNode) weave.DeltaItem {
		return weave.DeltaItem{ID: n.ID, Kind: n.Kind, Name: n.Name, File: n.File}
	}
	out := &weave.ExploreDelta{Ref: ref, Added: []weave.DeltaItem{}, Removed: []weave.DeltaItem{}, Moved: []weave.DeltaItem{}}
	for _, n := range d.added {
		out.Added = append(out.Added, item(n))
	}
	for _, n := range d.removed {
		it := item(n)
		if n.Kind == weave.KindEndpoint {
			it.Callers = d.callers[n.ID]
		}
		out.Removed = append(out.Removed, it)
	}
	for _, n := range d.moved {
		it := item(n)
		it.From = d.before.byID[n.ID].File
		out.Moved = append(out.Moved, it)
	}
	return out
}

// Delta compares current against base. all raises each section's cap from 12 to 60.
func Delta(current, base *weave.Graph, name string, all bool) string {
	cap := 12
	if all {
		cap = 60
	}
	d := diff(current, base)
	added, removed, moved, before, callers := d.added, d.removed, d.moved, d.before, d.callers

	out := []string{"delta vs " + name}
	if len(added) > 0 {
		out = append(out, "added "+strconv.Itoa(len(added)))
		for _, n := range first(added, cap) {
			out = append(out, "+ "+location(n))
		}
		if len(added) > cap {
			out = append(out, "  +"+strconv.Itoa(len(added)-cap)+" more (--all)")
		}
	}
	if len(removed) > 0 {
		out = append(out, "removed "+strconv.Itoa(len(removed)))
		for _, n := range first(removed, cap) {
			line := "- " + location(n)
			if n.Kind == weave.KindEndpoint {
				if c := callers[n.ID]; c != 0 {
					line += "  still called by " + strconv.Itoa(c) + " in the base"
				}
			}
			out = append(out, line)
		}
		if len(removed) > cap {
			out = append(out, "  +"+strconv.Itoa(len(removed)-cap)+" more (--all)")
		}
	}
	if len(moved) > 0 {
		out = append(out, "moved "+strconv.Itoa(len(moved)))
		for _, n := range first(moved, cap) {
			out = append(out, "~ "+jsstr.PadEnd(n.Kind, 9)+" "+n.Name+"  "+fileOf(before.byID[n.ID].File)+" → "+fileOf(n.File))
		}
		if len(moved) > cap {
			out = append(out, "  +"+strconv.Itoa(len(moved)-cap)+" more (--all)")
		}
	}
	if len(added) == 0 && len(removed) == 0 && len(moved) == 0 {
		out = append(out, "no change in endpoints, pages, tables, modules, requirements or changes")
	}
	out = append(out, strconv.Itoa(len(added))+" added · "+strconv.Itoa(len(removed))+" removed · "+strconv.Itoa(len(moved))+" moved")
	return strings.Join(out, "\n")
}

func first(ns []*weave.GNode, n int) []*weave.GNode {
	if len(ns) > n {
		return ns[:n]
	}
	return ns
}

// location is `kind.padEnd(9) name  file[:line]`, omitting an absent or empty file.
func location(n *weave.GNode) string {
	s := jsstr.PadEnd(n.Kind, 9) + " " + n.Name
	if n.File != nil && *n.File != "" {
		s += "  " + *n.File
		if n.Line != nil && *n.Line != 0 {
			s += ":" + strconv.Itoa(*n.Line)
		}
	}
	return s
}

// sameFile is `a !== b` on optional strings: undefined equals only undefined.
func sameFile(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// fileOf is `${p}`: an absent file prints "undefined", as a template literal does.
func fileOf(p *string) string {
	if p == nil {
		return "undefined"
	}
	return *p
}
