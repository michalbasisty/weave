package weave

import (
	"strings"
)

// NestJS declares routes with decorators, and Fastify with a route object:
//
//	@Controller('users') export class UsersController {
//	  @Get(':id') findOne() {}            → GET /users/:id
//	}
//	app.setGlobalPrefix('api')            → every Nest route under /api
//	fastify.route({ method: 'POST', url: '/orders', handler })

var (
	reNestController = jsRe(`@Controller\s*\(\s*(?:['"\x60]([^'"\x60]*)['"\x60]|\{[^}]*?path\s*:\s*['"\x60]([^'"\x60]*)['"\x60][^}]*\})?[^)]*\)`)
	reNestMethod     = jsRe(`@(Get|Post|Put|Patch|Delete|All|Options|Head)\s*\(\s*(?:['"\x60]([^'"\x60]*)['"\x60])?\s*\)`)
	reNestPrefix     = jsRe(`\.setGlobalPrefix\s*\(\s*['"\x60]([^'"\x60]+)['"\x60]`)
	reRouteObject    = jsRe(`\.route\s*\(\s*\{[^}]*?method\s*:\s*\[?\s*['"\x60]([A-Za-z]+)['"\x60][^}]*?url\s*:\s*['"\x60](\/[^'"\x60]*)['"\x60]`)
	reRouteObjectRev = jsRe(`\.route\s*\(\s*\{[^}]*?url\s*:\s*['"\x60](\/[^'"\x60]*)['"\x60][^}]*?method\s*:\s*\[?\s*['"\x60]([A-Za-z]+)['"\x60]`)
)

func joinRoute(parts ...string) string {
	var segs []string
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			segs = append(segs, p)
		}
	}
	return NormalizePath("/" + strings.Join(segs, "/"))
}

// ExtractNestAndFastify reads Nest controllers and Fastify route objects.
func ExtractNestAndFastify(files []SourceFile, b *Builder) []Endpoint {
	var out []Endpoint
	prefix := ""
	for i := range files {
		if m := reNestPrefix.FindStringSubmatch(files[i].Text); m != nil && isCode(files[i].Rel) {
			prefix = m[1]
			break
		}
	}
	add := func(f *SourceFile, method, path string, at int, framework string) {
		line := lineAt(f.Text, at)
		mod := moduleOf(f.Rel)
		eid := EndpointID(method, path)
		b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: method + " " + path, File: Ptr(f.Rel), Line: Ptr(line), Meta: strMeta("module", mod, "desc", CommentAbove(f.Lines, line, 220), "framework", framework)})
		b.Edge(eid, FileID(f.Rel), "defined_in")
		if mod != "" {
			b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(DirOf(f.Rel))})
			b.Edge(eid, ModuleID(mod), "in_module")
			b.Edge(ModuleID(mod), FileID(f.Rel), "contains")
		}
		out = append(out, Endpoint{method, path, f.Rel, line, mod, eid})
	}
	for i := range files {
		f := &files[i]
		if !isCode(f.Rel) || reTestOrSpec.MatchString(f.Rel) {
			continue
		}
		if strings.Contains(f.Text, "@Controller") {
			ctrls := reNestController.FindAllStringSubmatchIndex(f.Text, -1)
			for ci, c := range ctrls {
				base := ""
				if c[2] >= 0 {
					base = f.Text[c[2]:c[3]]
				} else if c[4] >= 0 {
					base = f.Text[c[4]:c[5]]
				}
				end := len(f.Text)
				if ci+1 < len(ctrls) {
					end = ctrls[ci+1][0]
				}
				for _, m := range reNestMethod.FindAllStringSubmatchIndex(f.Text[c[1]:end], -1) {
					verb := strings.ToUpper(f.Text[c[1]+m[2] : c[1]+m[3]])
					if verb == "ALL" {
						verb = "ANY"
					}
					sub := ""
					if m[4] >= 0 {
						sub = f.Text[c[1]+m[4] : c[1]+m[5]]
					}
					add(f, verb, joinRoute(prefix, base, sub), c[1]+m[0], "nest")
				}
			}
		}
		if strings.Contains(f.Text, ".route(") {
			seen := map[int]bool{}
			for _, m := range reRouteObject.FindAllStringSubmatchIndex(f.Text, -1) {
				seen[m[0]] = true
				add(f, strings.ToUpper(f.Text[m[2]:m[3]]), NormalizePath(f.Text[m[4]:m[5]]), m[0], "fastify")
			}
			for _, m := range reRouteObjectRev.FindAllStringSubmatchIndex(f.Text, -1) {
				if !seen[m[0]] {
					add(f, strings.ToUpper(f.Text[m[4]:m[5]]), NormalizePath(f.Text[m[2]:m[3]]), m[0], "fastify")
				}
			}
		}
	}
	return out
}
