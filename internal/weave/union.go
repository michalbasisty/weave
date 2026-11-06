package weave

import (
	"regexp"
	"strings"
)

// A template segment typed as a union of string literals is not "any item", it is
// one of a few fixed routes:
//
//	type TaskAction = 'start' | 'stop'
//	const act = (id: string, action: TaskAction) => api.put(`/tasks/${id}/${action}`)
//
// is two calls, PUT /tasks/*/start and PUT /tasks/*/stop. Reading it as
// `/tasks/:id/:action` matched no endpoint and failed the gate on working code.

var (
	reUnionAlias = regexp.MustCompile(`\btype\s+([A-Za-z_$][\w$]*)\s*=\s*\|?\s*((?:['"][^'"\n]*['"]\s*\|?\s*)+)`)
	reUnionLit   = regexp.MustCompile(`['"]([^'"\n]*)['"]`)
	reIdent      = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)
)

// unionAliases maps each `type T = 'a' | 'b'` in the repo to its values.
func unionAliases(files []SourceFile) map[string][]string {
	out := map[string][]string{}
	for i := range files {
		f := &files[i]
		if !isCode(f.Rel) || !strings.Contains(f.Text, "type ") {
			continue
		}
		for _, m := range reUnionAlias.FindAllStringSubmatch(f.Text, -1) {
			if _, dup := out[m[1]]; dup {
				continue
			}
			out[m[1]] = literals(m[2])
		}
	}
	return out
}

func literals(s string) []string {
	var out []string
	for _, m := range reUnionLit.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// unionValues is what the identifier is typed as in this file, when that is a union
// of string literals: inline (`action: 'start' | 'stop'`, two or more members, so an
// object property `{ action: 'x' }` is not mistaken for a type) or a named alias.
func unionValues(text, name string, aliases map[string][]string) []string {
	re, err := regexp.Compile(`(?:^|[^\w$.])` + regexp.QuoteMeta(name) + `\??\s*:\s*((?:['"][^'"\n]*['"]\s*\|?\s*)+|[A-Za-z_$][\w$]*)`)
	if err != nil {
		return nil
	}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		t := strings.TrimSpace(m[1])
		if reIdent.MatchString(t) {
			if v := aliases[t]; len(v) > 0 {
				return v
			}
			continue
		}
		if v := literals(t); len(v) >= 2 && strings.Contains(t, "|") {
			return v
		}
	}
	return nil
}

// expandUnion is the concrete paths of a call whose template segments are typed as
// literal unions, or nil when none is.
func expandUnion(c Call, text string, aliases map[string][]string) []string {
	if !strings.Contains(c.Path, "${") {
		return nil
	}
	paths := []string{c.Path}
	expanded := false
	for _, m := range reTemplateExpr.FindAllString(c.Path, -1) {
		name := strings.TrimSpace(m[2 : len(m)-1])
		if !reIdent.MatchString(name) {
			continue
		}
		vals := unionValues(text, name, aliases)
		if len(vals) == 0 || len(vals)*len(paths) > 64 {
			continue
		}
		var next []string
		for _, p := range paths {
			for _, v := range vals {
				next = append(next, strings.Replace(p, m, v, 1))
			}
		}
		paths, expanded = next, true
	}
	if !expanded {
		return nil
	}
	return paths
}

// variableLeafChoices covers a variable last segment whose type weave could not read
// (imported from a package, say) when two or more fixed routes sit at that spot:
// `/tasks/${id}/${action}` beside PUT /tasks/:id/start and /tasks/:id/stop is a
// choice between them, not a broken call. With one fixed sibling it stays unmatched:
// `/articles/${slug}` is an article, not `/articles/feed`.
func variableLeafChoices(c Call, endpoints []Endpoint) []*Endpoint {
	p := c.Path
	if q := strings.IndexByte(p, '?'); q >= 0 {
		p = p[:q]
	}
	slash := strings.LastIndexByte(p, '/')
	if slash < 0 || !strings.HasPrefix(p[slash+1:], "${") || !strings.HasSuffix(p, "}") {
		return nil
	}
	var out []*Endpoint
	seen := map[string]bool{}
	callSegs := segments(p)
	cs := len(callSegs)
	for i := range endpoints {
		// the same depth, or deeper by a mount prefix the call leaves out; every fixed
		// segment of the call sits on the same fixed segment of the route, so
		// `/articles/${x}` never lands on `/articles/:slug/comments`
		es := segments(endpoints[i].Path)
		if len(es) == 0 || len(es) < cs || !fixedAligned(callSegs, es) {
			continue
		}
		leaf := es[len(es)-1]
		if strings.HasPrefix(leaf, ":") || leaf == "*" || seen[endpoints[i].ID] {
			continue
		}
		v := c
		v.Path = p[:slash+1] + leaf
		if e := MatchEndpoint(v, endpoints); e != nil && e.ID == endpoints[i].ID {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func fixedAligned(call, route []string) bool {
	off := len(route) - len(call)
	for k, c := range call {
		if c == "*" || strings.Contains(c, "${") {
			continue
		}
		if route[off+k] != c {
			return false
		}
	}
	return true
}
