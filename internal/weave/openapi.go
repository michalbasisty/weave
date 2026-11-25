package weave

import (
	"regexp"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/yamljson"
)

// OpenAPI is the one contract that exists regardless of language, so it is the
// cheapest way out of "we only understand Express + Next + tRPC".

var (
	reSpecFile    = regexp.MustCompile(`(?i)(^|/)(openapi|swagger)(\.\d+)?\.json$`)
	reSpecYAML    = regexp.MustCompile(`(?i)(^|/)(openapi|swagger|api[-_.]?spec|spec)(\.\d+)?\.ya?ml$`)
	reSpecYAMLAPI = regexp.MustCompile(`(?i)(^|/)(openapi|swagger)(\.\d+)?\.ya?ml$`)
	// A top-level OpenAPI/Swagger key, used when the YAML cannot be parsed. Quoted
	// keys are legal; indented ones are allowed because this only proves intent.
	reYAMLSpecMarker = regexp.MustCompile(`(?m)^[ \t]*(?:"(?:openapi|swagger)"|'(?:openapi|swagger)'|(?:openapi|swagger))[ \t]*:`)
	reBraceParm      = regexp.MustCompile(`\{([^}]+)\}`)
	reEndSlash       = regexp.MustCompile(`/$`)
	apiMethods       = []string{"get", "post", "put", "patch", "delete", "head", "options"}
)

// SpecMaxBytes is the size a spec may reach: the general 512 KB cap would drop the
// large generated specs (Stripe, GitHub), and a dropped spec is a silent gap.
const SpecMaxBytes = 4 << 20

// specFile is a file named like an API spec. The walk admits it so the spec can
// be read, or its gap reported, instead of assuming no contract is there.
func specFile(rel string) bool { return reSpecFile.MatchString(rel) || reSpecYAML.MatchString(rel) }

// isSpecText is the cheap test that lets a spec be recognised by its content, not
// its name: repos commit `api/spec.json`, `docs/petstore.json`, `openapi-1.0.json`.
func isSpecText(t string) bool {
	return strings.Contains(t, `"paths"`) && (strings.Contains(t, `"openapi"`) || strings.Contains(t, `"swagger"`))
}

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

// isSpecDoc reports whether a parsed document declares itself an OpenAPI or
// Swagger document. A spec.yaml is a common name for other tools' files, so a
// YAML file that parses but makes no such claim is not a gap in the API map.
func isSpecDoc(v jsjson.Value) bool {
	for _, k := range []string{"openapi", "swagger"} {
		if x, ok := prop(v, k); truthy(x, ok) {
			return true
		}
	}
	return false
}

// hasYAMLSpecMarker finds the same claim in text that did not parse, so a broken
// OpenAPI spec is still reported while another tool's broken YAML is not.
func hasYAMLSpecMarker(t string) bool {
	return reYAMLSpecMarker.MatchString(strings.TrimPrefix(t, "\uFEFF"))
}

// ExtractOpenAPI reads OpenAPI/Swagger JSON and YAML specs, wherever they are
// named, and reports a spec it cannot parse as a silent file instead of a gap.
func ExtractOpenAPI(files []SourceFile, b *Builder) ExtractStats {
	st := ExtractStats{Silent: []string{}}
	for i := range files {
		f := &files[i]
		if len(f.Text) > SpecMaxBytes {
			continue
		}
		var spec jsjson.Value
		switch {
		case reSpecFile.MatchString(f.Rel), strings.HasSuffix(f.Rel, ".json") && isSpecText(f.Text):
			// A `.json` spec is still read as JSON, not through the looser YAML
			// grammar, so malformed JSON is reported rather than quietly accepted.
			st.Files++
			v, err := jsjson.Parse([]byte(f.Text))
			if err != nil {
				st.Silent = append(st.Silent, f.Rel)
				continue
			}
			spec = v
		case reSpecYAML.MatchString(f.Rel):
			v, err := yamljson.Parse([]byte(f.Text))
			if err != nil {
				// A generic spec.yaml may belong to another tool; only a clear
				// OpenAPI name or its own marker makes a parse failure a gap.
				if reSpecYAMLAPI.MatchString(f.Rel) || hasYAMLSpecMarker(f.Text) {
					st.Files++
					st.Silent = append(st.Silent, f.Rel)
				}
				continue
			}
			if !isSpecDoc(v) {
				continue
			}
			st.Files++
			spec = v
		default:
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
