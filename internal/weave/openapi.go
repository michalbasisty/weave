package weave

import (
	"regexp"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// OpenAPI is the one contract that exists regardless of language, so it is the
// cheapest way out of "we only understand Express + Next + tRPC".

var (
	reSpecFile  = regexp.MustCompile(`(?i)(^|/)(openapi|swagger)(\.\d+)?\.json$`)
	reBraceParm = regexp.MustCompile(`\{([^}]+)\}`)
	reEndSlash  = regexp.MustCompile(`/$`)
	apiMethods  = []string{"get", "post", "put", "patch", "delete", "head", "options"}
)

// truthy is JavaScript truthiness for a parsed JSON value.
func truthy(v jsjson.Value, present bool) bool {
	if !present {
		return false
	}
	switch v.Kind() {
	case jsjson.KindNull:
		return false
	case jsjson.KindBool:
		return v.Bool()
	case jsjson.KindNumber:
		return v.Float() != 0
	case jsjson.KindString:
		return v.Str() != ""
	}
	return true
}

// prop is v[k] for an object; arrays and scalars have no named properties here.
func prop(v jsjson.Value, k string) (jsjson.Value, bool) {
	if v.Kind() != jsjson.KindObject {
		return jsjson.Value{}, false
	}
	return v.Object().Get(k)
}

// firstTag is op.tags?.[0] ?? 'api' for the string tags specs use.
func firstTag(op jsjson.Value) string {
	tags, ok := prop(op, "tags")
	if !ok {
		return "api"
	}
	var first jsjson.Value
	switch tags.Kind() {
	case jsjson.KindArray:
		if len(tags.Array()) == 0 {
			return "api"
		}
		first = tags.Array()[0]
	case jsjson.KindString:
		if tags.Str() == "" {
			return "api"
		}
		return jsstr.Slice(tags.Str(), 0, 1)
	case jsjson.KindObject:
		v, ok := tags.Object().Get("0")
		if !ok {
			return "api"
		}
		first = v
	default:
		return "api"
	}
	switch first.Kind() {
	case jsjson.KindNull:
		return "api"
	case jsjson.KindString:
		return first.Str()
	case jsjson.KindNumber:
		return jsjson.FormatNumber(first.Float())
	case jsjson.KindBool:
		if first.Bool() {
			return "true"
		}
		return "false"
	}
	return "api" // divergence: an object or array tag stringifies differently in JS
}

// opDesc is `(op.summary || op.description || "").trim().slice(0, 220)` for string fields.
func opDesc(op jsjson.Value) string {
	for _, k := range []string{"summary", "description"} {
		v, ok := prop(op, k)
		if truthy(v, ok) {
			return jsstr.Slice(jsstr.Trim(v.Str()), 0, 220)
		}
	}
	return ""
}

// ExtractOpenAPI reads openapi.json / swagger.json files.
func ExtractOpenAPI(files []SourceFile, b *Builder) ExtractStats {
	st := ExtractStats{Silent: []string{}}
	for i := range files {
		f := &files[i]
		if !reSpecFile.MatchString(f.Rel) || len(f.Text) > 4*1024*1024 {
			continue
		}
		st.Files++
		spec, err := jsjson.Parse([]byte(f.Text))
		if err != nil {
			st.Silent = append(st.Silent, f.Rel)
			continue
		}
		paths, _ := prop(spec, "paths")
		here := 0
		if paths.Kind() == jsjson.KindObject {
			o := paths.Object()
			for k := 0; k < o.Len(); k++ {
				rawPath, item := o.At(k)
				if item.Kind() != jsjson.KindObject && item.Kind() != jsjson.KindArray {
					continue
				}
				path := reEndSlash.ReplaceAllString(reBraceParm.ReplaceAllString(rawPath, ":$1"), "")
				if path == "" {
					path = "/"
				}
				for _, method := range apiMethods {
					op, ok := prop(item, method)
					if !truthy(op, ok) {
						continue
					}
					tag := firstTag(op)
					desc := opDesc(op)
					upper := strings.ToUpper(method)
					eid := EndpointID(upper, path)
					b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: upper + " " + path, File: Ptr(f.Rel), Meta: strMeta("module", tag, "desc", desc, "framework", "openapi")})
					b.Edge(eid, FileID(f.Rel), "defined_in")
					b.Node(&GNode{ID: ModuleID(tag), Kind: KindModule, Name: tag, File: Ptr(f.Rel)})
					b.Edge(eid, ModuleID(tag), "in_module")
					st.Endpoints = append(st.Endpoints, Endpoint{upper, path, f.Rel, 0, tag, eid})
					here++
				}
			}
		}
		if here > 0 {
			st.Found++
		} else {
			st.Silent = append(st.Silent, f.Rel)
		}
	}
	return st
}
