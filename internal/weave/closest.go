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
		shape := shapeOf(p)
		d := levenshtein(want, shape)
		if m != method && m != "ANY" && method != "ANY" {
			d += 3
		}
		// close enough to be a guess: a small edit, or a rename that kept the stem
		// (`/tags` → `/tag-list`)
		limit := len(want) * 2 / 5
		if limit < 3 {
			limit = 3
		}
		if d > limit && !sameStem(want, shape) {
			continue
		}
		if best == "" || d < bestD || (d == bestD && n.Name < best) {
			best, bestD = n.Name, d
		}
	}
	if best == call {
		return ""
	}
	return best
}

// sameStem: the last fixed segments share a prefix of three letters or more, and
// everything before them is the same shape.
func sameStem(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	if len(as) != len(bs) {
		return false
	}
	last := len(as) - 1
	for last >= 0 && as[last] == ":" && bs[last] == ":" {
		last--
	}
	if last < 0 || strings.Join(as[:last], "/") != strings.Join(bs[:last], "/") {
		return false
	}
	x, y := as[last], bs[last]
	n := 0
	for n < len(x) && n < len(y) && x[n] == y[n] {
		n++
	}
	return n >= 3 && x != y
}

// shapeOf writes every variable segment the same way, so `/tasks/${id}` and
// `/tasks/:id` compare equal; the query string is not part of the route.
func shapeOf(p string) string {
	p = WithoutQuery(p)
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") || strings.HasPrefix(s, "${") || s == "*" || s == "**" || strings.HasPrefix(s, "{") || strings.HasPrefix(s, "<") {
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
