package weave

import (
	"regexp"
	"strings"
)

// Remix and React Router v7 publish their routes as file names under app/routes:
//
//	app/routes/_index.tsx                  → /
//	app/routes/api.render.tsx              → /api/render
//	app/routes/api+/preferred-team.tsx     → /api/preferred-team   (remix-flat-routes)
//	app/routes/_auth+/t.$teamUrl+/_index   → /t/:teamUrl
//	app/routes/blog.$slug/route.tsx        → /blog/:slug
//
// A module with a default export is a screen. A module with only `loader` / `action`
// is a resource route: loader answers GET, action answers the other verbs.

var (
	reRemixRoutes = regexp.MustCompile(`^(?:(.*)/)?app/routes/(.+)\.(?:tsx|ts|jsx|js|mdx)$`)
	reRemixLoader = jsRe(`export\s+(?:async\s+)?(?:function|const|let)\s+loader\b`)
	reRemixAction = jsRe(`export\s+(?:async\s+)?(?:function|const|let)\s+action\b`)
	reRemixExport = jsRe(`export\s*\{[^}]*\b(loader|action|default)\b[^}]*\}`)
)

// remixRoute is the URL a module under app/routes serves, when that folder sits
// next to an app/root module (Remix, React Router v7); else ok is false.
func remixRoute(rel string, known map[string]bool) (route string, ok bool) {
	m := reRemixRoutes.FindStringSubmatch(rel)
	if m == nil {
		return "", false
	}
	base := "app/"
	if m[1] != "" {
		base = m[1] + "/app/"
	}
	root := false
	for _, ext := range []string{"tsx", "jsx", "ts", "js"} {
		if known[base+"root."+ext] {
			root = true
			break
		}
	}
	if !root {
		return "", false
	}
	name := m[2]
	// colocated helpers are not routes
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, "+") || strings.Contains(part, ".server") || strings.Contains(part, ".client") {
			return "", false
		}
	}
	// folder routes: blog.$slug/route.tsx, blog/index.tsx
	if strings.HasSuffix(name, "/route") || strings.HasSuffix(name, "/index") {
		name = name[:strings.LastIndexByte(name, '/')]
	} else if i := strings.LastIndexByte(name, '/'); i >= 0 && !strings.HasSuffix(name[:i], "+") {
		// a file inside a plain folder that is not the folder's route module
		return "", false
	}
	name = strings.ReplaceAll(name, "+/", ".")
	var parts []string
	for _, seg := range splitRemix(name) {
		switch {
		case seg == "" || seg == "_index" || seg == "index" || seg == "route":
			continue
		case strings.HasPrefix(seg, "_"): // pathless layout
			continue
		case strings.HasPrefix(seg, "(") && strings.HasSuffix(seg, ")"): // optional segment
			continue
		}
		seg = strings.TrimSuffix(seg, "_") // escape from the parent layout
		switch {
		case seg == "$":
			parts = append(parts, "*")
		case strings.HasPrefix(seg, "$"):
			parts = append(parts, ":"+seg[1:])
		default:
			parts = append(parts, strings.NewReplacer("[", "", "]", "").Replace(seg))
		}
	}
	return NormalizePath("/" + strings.Join(parts, "/")), true
}

// splitRemix splits on dots that are not inside [brackets].
func splitRemix(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// ExtractRemixRoutes reads app/routes modules: resource routes become endpoints,
// modules with a default export become screens.
func ExtractRemixRoutes(files []SourceFile, b *Builder) (routes, pages ExtractStats) {
	routes = ExtractStats{Silent: []string{}}
	pages = ExtractStats{Silent: []string{}}
	known := knownSet(files)
	for i := range files {
		f := &files[i]
		route, ok := remixRoute(f.Rel, known)
		if !ok {
			continue
		}
		loader := reRemixLoader.MatchString(f.Text)
		action := reRemixAction.MatchString(f.Text)
		def := reExportDefault.MatchString(f.Text)
		for _, m := range reRemixExport.FindAllStringSubmatch(f.Text, -1) {
			switch m[1] {
			case "loader":
				loader = true
			case "action":
				action = true
			case "default":
				def = true
			}
		}
		if def {
			pages.Files++
			pid := PageID(route)
			b.Node(&GNode{ID: pid, Kind: KindPage, Name: route, File: Ptr(f.Rel), Meta: strMeta("desc", TopComment(f.Lines, 220), "framework", "react-router")})
			b.Edge(pid, FileID(f.Rel), "renders")
			pages.Found++
			continue
		}
		routes.Files++
		var methods []string
		if loader {
			methods = append(methods, "GET")
		}
		if action {
			methods = append(methods, "ANY") // action answers every non-GET verb
		}
		if len(methods) == 0 {
			routes.Silent = append(routes.Silent, f.Rel)
			continue
		}
		mod := "routes"
		b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(f.Rel)})
		desc := CommentAbove(f.Lines, 1, 220)
		for _, method := range methods {
			eid := EndpointID(method, route)
			b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: method + " " + route, File: Ptr(f.Rel), Meta: strMeta("module", mod, "desc", desc, "framework", "react-router")})
			b.Edge(eid, FileID(f.Rel), "defined_in")
			b.Edge(eid, ModuleID(mod), "in_module")
			b.Edge(ModuleID(mod), FileID(f.Rel), "contains")
			routes.Endpoints = append(routes.Endpoints, Endpoint{method, route, f.Rel, 0, mod, eid})
		}
		routes.Found++
	}
	return routes, pages
}
