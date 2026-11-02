package weave

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// Per-repo extraction patterns. Every mid-size team has a wrapper the built-in
// regexes do not know (`defineRoute()`, a generated client, `request()`); this is
// the escape hatch that costs five lines of config instead of a rewrite.

// PatternRule is one entry of .weave/patterns.json: capture group 1 is the path.
type PatternRule struct {
	Match  string
	Method *string // nil when absent or null: ANY
}

// Patterns is .weave/patterns.json.
type Patterns struct {
	Routes, Calls []PatternRule
}

// CompiledRule is a rule ready to run.
type CompiledRule struct {
	Re     *regexp.Regexp
	Method string
}

// Compiled is Patterns with every valid rule compiled.
type Compiled struct {
	Routes, Calls []CompiledRule
}

// LoadPatterns reads <root>/.weave/patterns.json; a missing or broken file is empty.
func LoadPatterns(root string) Patterns {
	raw, err := os.ReadFile(filepath.Join(root, ".weave", "patterns.json"))
	if err != nil {
		return Patterns{}
	}
	v, err := jsjson.Parse([]byte(jsstr.DecodeUTF8(raw)))
	if err != nil || v.Kind() == jsjson.KindNull {
		return Patterns{} // JSON.parse threw, or `null.routes` did
	}
	rules := func(k string) []PatternRule {
		list, ok := prop(v, k)
		if !ok || list.Kind() != jsjson.KindArray {
			return nil
		}
		var out []PatternRule
		for _, r := range list.Array() {
			m, ok := prop(r, "match")
			if !ok || m.Kind() != jsjson.KindString {
				continue
			}
			rule := PatternRule{Match: m.Str()}
			if meth, ok := prop(r, "method"); ok && meth.Kind() != jsjson.KindNull {
				if meth.Kind() != jsjson.KindString {
					rule.Method = Ptr("\x00invalid") // (r.method).toUpperCase() throws: compile drops it
				} else {
					rule.Method = Ptr(meth.Str())
				}
			}
			out = append(out, rule)
		}
		return out
	}
	return Patterns{Routes: rules("routes"), Calls: rules("calls")}
}

// CompilePatterns compiles every rule; a broken pattern must not break the index.
// Divergence: a JavaScript-only construct (lookaround, backreference) fails to
// compile here and the rule is dropped, where Node would run it.
func CompilePatterns(p Patterns) Compiled {
	build := func(rules []PatternRule) []CompiledRule {
		var out []CompiledRule
		for _, r := range rules {
			method := "ANY"
			if r.Method != nil {
				if *r.Method == "\x00invalid" {
					continue
				}
				method = strings.ToUpper(*r.Method)
			}
			re, err := regexp.Compile(jsSource(r.Match))
			if err != nil {
				continue
			}
			out = append(out, CompiledRule{re, method})
		}
		return out
	}
	return Compiled{Routes: build(p.Routes), Calls: build(p.Calls)}
}

// PatternHit is one match of a custom rule.
type PatternHit struct {
	Path, Method, File string
	Line               int
}

func patternHits(files []SourceFile, rules []CompiledRule, route bool) []PatternHit {
	var out []PatternHit
	if len(rules) == 0 {
		return out
	}
	for i := range files {
		f := &files[i]
		if !isCode(f.Rel) {
			continue
		}
		for _, r := range rules {
			for _, m := range r.Re.FindAllStringSubmatchIndex(f.Text, -1) {
				if len(m) < 4 || m[2] < 0 || m[3] == m[2] {
					continue
				}
				raw := f.Text[m[2]:m[3]]
				if route && !strings.HasPrefix(raw, "/") {
					raw = "/" + raw
				}
				out = append(out, PatternHit{raw, r.Method, f.Rel, lineAt(f.Text, m[0])})
			}
		}
	}
	return out
}

// PatternRoutes is every route a custom rule declares.
func PatternRoutes(files []SourceFile, c Compiled) []PatternHit {
	return patternHits(files, c.Routes, true)
}

// PatternCalls is every call a custom rule finds.
func PatternCalls(files []SourceFile, c Compiled) []PatternHit {
	return patternHits(files, c.Calls, false)
}
