package weave

import (
	"strings"
)

// Closest is the endpoint a broken call most likely meant, for the FAIL line:
// "PUT /tasks/${id}/pause matches no endpoint; closest PUT /tasks/:id/stop". An agent
// can fix the call from that without searching. "" when nothing is close enough to
// be a useful guess.
func Closest(g *Graph, call string) string {
	method, path, ok := strings.Cut(call, " ")
	if !ok {
		return ""
	}
	want := shapeOf(path)
	best, bestD := "", 1<<30
	for _, n := range g.Nodes {
		if n.Kind != KindEndpoint {
			continue
		}
		m, p, ok := strings.Cut(n.Name, " ")
		if !ok {
			continue
		}
		d := levenshtein(want, shapeOf(p))
		if m != method && m != "ANY" && method != "ANY" {
			d += 3
		}
		if d < bestD || (d == bestD && n.Name < best) {
			best, bestD = n.Name, d
		}
	}
	limit := len(want) * 2 / 5
	if limit < 3 {
		limit = 3
	}
	if best == "" || bestD > limit || bestD == 0 && best == call {
		return ""
	}
	return best
}

// shapeOf writes every variable segment the same way, so `/tasks/${id}` and
// `/tasks/:id` compare equal; the query string is not part of the route.
func shapeOf(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") || strings.HasPrefix(s, "${") || s == "*" || strings.HasPrefix(s, "{") || strings.HasPrefix(s, "<") {
			segs[i] = ":"
		}
	}
	return strings.ToLower(strings.TrimRight(strings.Join(segs, "/"), "/"))
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(min(prev[j]+1, cur[j-1]+1), prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
