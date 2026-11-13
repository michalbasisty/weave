// Package verify is the gate: what a diff breaks, which endpoints it leaves
// untested, and the commands that prove it. `weave check` and the MCP `verify`
// tool both read one Result, so the two can never disagree.
package verify

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/junit"
	"github.com/michalbasisty/weave/internal/weave"
)

var refSanitise = regexp.MustCompile(`[^\w./~^-]`)
var gitPrefix = regexp.MustCompile(`^[AMDR?]{1,2}\s+`)

// Result is everything the gate found. Scope is the text after `check: `.
type Result struct {
	Scope    string
	Errors   []string
	Warnings []string
	Commands []string
	Changes  []string
	// Broken lists each removed endpoint that something still calls, with the
	// screens that reach the caller. Pages and Endpoints are what the change touches.
	Broken    []Broken
	Pages     []string
	Endpoints []string
	// Diagram is a Mermaid picture of the blast radius, "" when there is none.
	Diagram string
	// Blast is the same blast radius as data, uncapped.
	Blast Blast
}

// Broken is one removed endpoint that a live call site still names.
type Broken struct {
	Endpoint string
	Caller   string
	Screens  []string
	Closest  string // the route the call most likely meant, "" when none is near
}

// Text renders the result the way `weave check` prints it: one scope line, then
// any FAIL, warn and run lines.
func (r Result) Text() string {
	lines := make([]string, 0, 1+len(r.Errors)+len(r.Warnings)+len(r.Commands))
	lines = append(lines, "check: "+r.Scope)
	for _, e := range r.Errors {
		lines = append(lines, "  FAIL "+e)
	}
	for _, w := range r.Warnings {
		lines = append(lines, "  warn "+w)
	}
	for _, c := range r.Commands {
		lines = append(lines, "  run  "+c)
	}
	return strings.Join(lines, "\n")
}

// Run gates an already-built graph. An empty files list means the git diff, and
// an empty against means the last local index.
func Run(g *weave.Graph, root, against string, strict bool, files []string) Result {
	against = refSanitise.ReplaceAllString(against, "")
	if len(files) == 0 {
		files = gitChanged(root, against)
	}
	res := Result{Changes: files}
	var f *weave.Facts
	if len(files) > 0 {
		f = weave.Analyze(g, files)
	}

	var errors, warnings []string

	var prev []string
	var baseGraph *weave.Graph // the graph removed endpoints are read from, when there is one
	if against != "" {
		if base := baseline.GraphAt(root, against); base != nil {
			baseGraph = base
			prev = endpointNames(base)
		} else {
			warnings = append(warnings, fmt.Sprintf("could not read %s — comparing against the last local index instead", against))
			prev = g.PrevEndpoints
		}
	} else {
		prev = g.PrevEndpoints
		// The last index moves with every run, so a second `weave check` on the same
		// edit would compare the edit with itself and pass. The committed tree does
		// not move: with uncommitted changes, an endpoint gone since HEAD counts too.
		if len(gitChanged(root, "")) > 0 {
			if base := baseline.GraphAt(root, "HEAD"); base != nil {
				baseGraph = base
				seen := make(map[string]bool, len(prev))
				for _, e := range prev {
					seen[e] = true
				}
				for _, e := range endpointNames(base) {
					if !seen[e] {
						prev = append(prev, e)
						seen[e] = true
					}
				}
			}
		}
	}
	now := endpointNames(g)
	nowSet := make(map[string]bool, len(now))
	for _, e := range now {
		nowSet[e] = true
	}
	var removed []string
	for _, e := range prev {
		if !nowSet[e] {
			removed = append(removed, e)
		}
	}
	// What this change added: against the committed tree when it is at hand,
	// since the last local index may already hold the edit.
	before := prev
	if baseGraph != nil {
		before = endpointNames(baseGraph)
	}
	prevSet := make(map[string]bool, len(before))
	for _, e := range before {
		prevSet[e] = true
	}
	var added []string
	for _, e := range now {
		if !prevSet[e] {
			added = append(added, e)
		}
	}

	type brokenCall struct {
		gone string
		call *weave.GNode
	}
	var broken []brokenCall
	for _, n := range g.Nodes {
		if n.Kind != weave.KindCall {
			continue
		}
		for _, r := range removed {
			if sameShape(r, n.Name) {
				broken = append(broken, brokenCall{gone: r, call: n})
				break
			}
		}
	}
	// The screens a removed endpoint served are the ones that called it before:
	// exact when the old graph is at hand, else every screen that reaches the caller.
	calledBy := map[string][]string{}
	if baseGraph != nil {
		for _, e := range baseGraph.Edges {
			if e.Kind == "calls" && strings.HasPrefix(e.From, "page:") && strings.HasPrefix(e.To, "endpoint:") {
				calledBy[strings.TrimPrefix(e.To, "endpoint:")] = append(calledBy[strings.TrimPrefix(e.To, "endpoint:")], strings.TrimPrefix(e.From, "page:"))
			}
		}
	}
	screensOf := map[string][]string{}
	for _, b := range broken {
		var screens []string
		if s, ok := calledBy[b.gone]; ok {
			screens = s
		} else if b.call.File != nil {
			file := *b.call.File
			if s, ok := screensOf[file]; ok {
				screens = s
			} else if fa := weave.Analyze(g, []string{file}); fa != nil {
				for _, p := range fa.Pages {
					screens = append(screens, p.Name)
				}
			}
			screensOf[file] = screens
		}
		res.Broken = append(res.Broken, Broken{b.gone, locNode(b.call), screens, renamedTo(g, b.gone, added)})
	}
	if len(broken) > 0 {
		var parts []string
		for i, b := range broken {
			if i == 4 {
				break
			}
			part := fmt.Sprintf("%s from %s", b.gone, locNode(b.call))
			if c := res.Broken[i].Closest; c != "" {
				part += " (closest now: " + c + ")"
			}
			parts = append(parts, part)
		}
		errors = append(errors, "removed endpoint still called: "+strings.Join(parts, "; "))
	}
	if len(removed) > 0 && len(broken) == 0 {
		shown := removed
		if len(shown) > 4 {
			shown = shown[:4]
		}
		warnings = append(warnings, fmt.Sprintf("removed %d endpoint(s), no live caller: %s", len(removed), strings.Join(shown, ", ")))
	}

	if f != nil {
		if len(f.Orphans) > 0 {
			var shown []string
			for _, o := range head(f.Orphans, 4) {
				if c := weave.Closest(g, o); c != "" {
					o += " (closest: " + c + ")"
				}
				shown = append(shown, o)
			}
			errors = append(errors, fmt.Sprintf("call site(s) in changed files match no endpoint: %s", strings.Join(shown, ", ")))
		}
		if len(f.Suspect) > 0 {
			warnings = append(warnings, fmt.Sprintf("requirement text changed, code not re-checked: %s", strings.Join(head(f.Suspect, 4), ", ")))
		}
		if len(f.Changes) == 0 {
			warnings = append(warnings, "no spec change record names these files")
		}
		if len(f.Untested) > 0 {
			msg := fmt.Sprintf("%d touched endpoint(s) have no direct test", len(f.Untested))
			if strict {
				errors = append(errors, msg)
			} else {
				warnings = append(warnings, msg)
			}
		}

		cases := junit.Load(root)
		if len(cases) > 0 {
			ev := weave.EvidenceFacts(g, cases, f.Reqs)
			var failing, unverified []weave.ReqEvidence
			for _, e := range ev {
				if e.Fail > 0 {
					failing = append(failing, e)
				}
				if e.Unverified {
					unverified = append(unverified, e)
				}
			}
			if len(failing) > 0 {
				var parts []string
				for i, e := range failing {
					if i == 3 {
						break
					}
					test := ""
					if len(e.Tests) > 0 {
						test = e.Tests[0]
					}
					parts = append(parts, fmt.Sprintf("%s (%s)", e.ID, test))
				}
				errors = append(errors, "requirement test failing: "+strings.Join(parts, "; "))
			}
			if len(unverified) > 0 {
				var ids []string
				for i, e := range unverified {
					if i == 4 {
						break
					}
					ids = append(ids, e.ID)
				}
				warnings = append(warnings, "implemented but never tested: "+strings.Join(ids, ", "))
			}
		} else if len(f.Reqs) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d requirement(s) linked but no JUnit report — evidence not checked", len(f.Reqs)))
		}
		res.Commands = f.Commands
		for _, p := range f.Pages {
			res.Pages = append(res.Pages, p.Name)
		}
		for _, e := range f.Endpoints {
			res.Endpoints = append(res.Endpoints, e.Name)
		}
	}

	scope := ""
	if f != nil {
		scope = fmt.Sprintf("%d file(s) → %d endpoint(s), %d page(s), %d test(s)", len(f.Files), len(f.Endpoints), len(f.Pages), len(f.Tests))
	} else {
		scope = fmt.Sprintf("%d endpoint(s) indexed, no changed file named", len(now))
	}
	extra := ""
	if against != "" {
		extra += " vs " + against
	}
	if len(removed) > 0 {
		extra += fmt.Sprintf(", %d endpoint(s) removed", len(removed))
	}
	res.Diagram = blastDiagram(g, f, res.Broken)
	res.Blast = NewBlast(g, f, res.Broken)
	res.Scope = scope + extra
	res.Errors = errors
	res.Warnings = warnings
	return res
}

func gitChanged(root, against string) []string {
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	rangeSpec := "HEAD"
	if against != "" {
		rangeSpec = against + "...HEAD"
	}
	out := run("diff", "--name-only", rangeSpec)
	if out == "" && against == "" {
		out = run("status", "--porcelain")
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = gitPrefix.ReplaceAllString(line, "")
		if line != "" && !strings.HasPrefix(line, ".weave/") {
			files = append(files, line)
		}
	}
	return files
}

func endpointNames(g *weave.Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == weave.KindEndpoint {
			out = append(out, n.Name)
		}
	}
	return out
}

// sameShape: call sites are relative to the mount, so compare tail segments.
// A parameter segment matches anything, so a match needs at least one literal
// segment in common: without that, `/stats` matched `users/:id` and a commit that
// removed an unrelated endpoint was reported as breaking a caller.
func sameShape(a, b string) bool {
	sa := strings.Fields(a)
	sb := strings.Fields(b)
	if len(sa) < 2 || len(sb) < 2 || (sa[0] != sb[0] && sa[0] != "ANY" && sb[0] != "ANY") {
		return false
	}
	// A call keeps its query string in its name (`/payment?plan=`); a route has none.
	xa := segments(weave.WithoutQuery(sa[1]))
	xb := segments(weave.WithoutQuery(sb[1]))
	n := len(xa)
	if len(xb) < n {
		n = len(xb)
	}
	if n == 0 {
		return false
	}
	literal := false
	for i := 1; i <= n; i++ {
		p := xa[len(xa)-i]
		q := xb[len(xb)-i]
		param := strings.HasPrefix(p, ":") || strings.HasPrefix(q, ":") || p == "*" || q == "*" || p == "**" || q == "**"
		if param || p == q {
			if !param && p == q {
				literal = true
			}
			continue
		}
		return false
	}
	return literal
}

func segments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func locNode(n *weave.GNode) string {
	file := "?"
	if n.File != nil {
		file = *n.File
	}
	if n.Line != nil && *n.Line > 0 {
		return fmt.Sprintf("%s:%d", file, *n.Line)
	}
	return file
}

func head[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}

// renamedTo guesses where a removed endpoint went. A route added by the same
// change, with the same method and under the same parent path, is the likeliest
// answer (`GET /orders` → `GET /orders/history`); otherwise the nearest route.
func renamedTo(g *weave.Graph, gone string, added []string) string {
	method, path, ok := strings.Cut(gone, " ")
	if !ok {
		return ""
	}
	parent := path
	if i := strings.LastIndex(strings.TrimSuffix(path, "/"), "/"); i >= 0 {
		parent = path[:i+1]
	}
	best, bestD := "", 1<<30
	for _, a := range added {
		m, p, ok := strings.Cut(a, " ")
		if !ok || m != method || !strings.HasPrefix(p, parent) {
			continue
		}
		if d := editDistance(path, p); d < bestD || (d == bestD && a < best) {
			best, bestD = a, d
		}
	}
	if best != "" {
		return best
	}
	return weave.Closest(g, gone)
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := prev[j-1]
			if a[i-1] != b[j-1] {
				c++
			}
			cur[j] = min(c, prev[j]+1, cur[j-1]+1)
		}
		prev = cur
	}
	return prev[len(b)]
}
