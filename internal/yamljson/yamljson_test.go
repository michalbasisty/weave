package yamljson

import (
	"strconv"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/jsjson"
)

func mustParse(t *testing.T, doc string) jsjson.Value {
	t.Helper()
	v, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return v
}

// A spec committed as YAML must reach the extractors as the same tree as the
// JSON version of the same spec.
func TestParseJSONParity(t *testing.T) {
	yamlDoc := `openapi: "3.0.0"
info:
  title: Pet API
  version: 1
paths:
  "/pets/{id}":
    get:
      tags: [pets, cats]
      summary: "Find a pet"
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: integer
            format: int64
  /health:
    get: {}
text: |
  first
  second
quoted: 'a # b'
answer: 42
hex: 0x10
ratio: 1.5
nothing: null
enabled: true
`
	jsonDoc := `{"openapi":"3.0.0","info":{"title":"Pet API","version":1},"paths":{"/pets/{id}":{"get":{"tags":["pets","cats"],"summary":"Find a pet","parameters":[{"in":"path","name":"id","required":true,"schema":{"type":"integer","format":"int64"}}]}},"/health":{"get":{}}},"text":"first\nsecond\n","quoted":"a # b","answer":42,"hex":16,"ratio":1.5,"nothing":null,"enabled":true}`

	want, err := jsjson.Parse([]byte(jsonDoc))
	if err != nil {
		t.Fatalf("jsjson.Parse: %v", err)
	}
	got := mustParse(t, yamlDoc)
	if !got.Equal(want) {
		t.Fatalf("YAML and JSON differ:\n got %s\nwant %s", dump(got), jsonDoc)
	}
}

// dump is a readable approximation for failure messages.
func dump(v jsjson.Value) string {
	switch v.Kind() {
	case jsjson.KindObject:
		var b strings.Builder
		b.WriteString("{")
		o := v.Object()
		for i := 0; i < o.Len(); i++ {
			if i > 0 {
				b.WriteString(",")
			}
			k, x := o.At(i)
			b.WriteString(strconv.Quote(k) + ":" + dump(x))
		}
		b.WriteString("}")
		return b.String()
	case jsjson.KindArray:
		var b strings.Builder
		b.WriteString("[")
		for i, x := range v.Array() {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(dump(x))
		}
		b.WriteString("]")
		return b.String()
	case jsjson.KindString:
		return strconv.Quote(v.Str())
	case jsjson.KindNumber:
		return v.Lexeme()
	case jsjson.KindBool:
		return strconv.FormatBool(v.Bool())
	default:
		return "null"
	}
}

// Mapping order decides which path is reported first, so it must survive.
func TestParsePreservesKeyOrder(t *testing.T) {
	v := mustParse(t, "zebra: 1\nalpha: 2\nmiddle: 3\n")
	o := v.Object()
	want := []string{"zebra", "alpha", "middle"}
	if len(o.Keys()) != len(want) {
		t.Fatalf("keys: got %v, want %v", o.Keys(), want)
	}
	for i := range want {
		if o.Keys()[i] != want[i] {
			t.Fatalf("keys: got %v, want %v", o.Keys(), want)
		}
	}
}

// Anchors are how real OpenAPI files share parameter and error definitions.
func TestParseExpandsAnchorsAndAliases(t *testing.T) {
	v := mustParse(t, `common: &common
  tags: [pets]
  summary: Shared
limit: &limit 25
paths:
  /a:
    get: *common
  /b:
    get: *common
  /c:
    get:
      limit: *limit
`)
	paths, _ := v.Object().Get("paths")
	common, _ := v.Object().Get("common")
	for _, p := range []string{"/a", "/b"} {
		item, _ := paths.Object().Get(p)
		get, _ := item.Object().Get("get")
		if !get.Equal(common) {
			t.Errorf("%s get: got %s, want the anchored mapping", p, dump(get))
		}
	}
	c, _ := paths.Object().Get("/c")
	get, _ := c.Object().Get("get")
	limit, _ := get.Object().Get("limit")
	if limit.Kind() != jsjson.KindNumber || limit.Float() != 25 {
		t.Errorf("scalar alias: got %s, want 25", dump(limit))
	}
}

func TestParseQuotedPathsAndNumericKeys(t *testing.T) {
	v := mustParse(t, `paths:
  "/pets/{id}":
    get: {}
responses:
  200:
    description: ok
  404:
    description: missing
`)
	paths, _ := v.Object().Get("paths")
	if _, ok := paths.Object().Get("/pets/{id}"); !ok {
		t.Fatalf("quoted path missing; keys %v", paths.Object().Keys())
	}
	responses, _ := v.Object().Get("responses")
	if got := responses.Object().Keys(); len(got) != 2 || got[0] != "200" || got[1] != "404" {
		t.Fatalf("numeric keys: got %v, want [200 404]", got)
	}
}

// Merge keys are the usual way a hand-written spec shares a block. Explicit
// keys win, and in a merge list the earlier map wins.
func TestParseResolvesMergeKeys(t *testing.T) {
	v := mustParse(t, `base: &base
  a: 1
  b: 2
override: &override
  b: 9
  c: 3
merged:
  <<: [*base, *override]
explicit:
  b: 20
  <<: *base
`)
	merged, _ := v.Object().Get("merged")
	mo := merged.Object()
	if got := mo.Keys(); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("merged keys: got %v, want [a b c]", got)
	}
	b, _ := mo.Get("b")
	if b.Float() != 2 {
		t.Errorf("merged b: got %s, want 2 from the earlier map", dump(b))
	}
	explicit, _ := v.Object().Get("explicit")
	eo := explicit.Object()
	if got := eo.Keys(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("explicit keys: got %v, want [b a]", got)
	}
	b, _ = eo.Get("b")
	if b.Float() != 20 {
		t.Errorf("explicit b: got %s, want 20", dump(b))
	}
}

func TestParseRejectsDuplicateKeys(t *testing.T) {
	_, err := Parse([]byte("paths:\n  /x: {}\n  /x: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("duplicate key: got %v, want a duplicate-key error", err)
	}
}

func TestParseRejectsMalformedYAML(t *testing.T) {
	for _, doc := range []string{
		"paths: [\n",
		"paths:\n\t/x: {}\n",
		"a: *missing\n",
		"a: &a [*a]\n",
		"a: &a\n  <<: *a\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("malformed %q: got nil error", doc)
		}
	}
}

// An alias bomb must fail on the budget, not by exhausting memory.
func TestParseBoundsAliasExpansion(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x]\n")
	for i := 1; i <= 12; i++ {
		b.WriteString("a" + strconv.Itoa(i) + ": &a" + strconv.Itoa(i) + " [")
		for j := 0; j < 6; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*a" + strconv.Itoa(i-1))
		}
		b.WriteString("]\n")
	}
	_, err := parse([]byte(b.String()), limits{depth: 200, nodes: 1000, aliasDepth: 100})
	if err == nil || !strings.Contains(err.Error(), "expanded document larger") {
		t.Fatalf("alias bomb: got %v, want a node-budget error", err)
	}
}

func TestParseBoundsDepth(t *testing.T) {
	doc := strings.Repeat("[", 300) + "x" + strings.Repeat("]", 300)
	_, err := parse([]byte(doc), limits{depth: 20, nodes: 10000, aliasDepth: 10})
	if err == nil || !strings.Contains(err.Error(), "nesting deeper") {
		t.Fatalf("deep nesting: got %v, want a depth error", err)
	}
}

func TestParseBoundsSize(t *testing.T) {
	data := make([]byte, MaxBytes+1)
	for i := range data {
		data[i] = 'a'
	}
	if _, err := Parse(data); err == nil || !strings.Contains(err.Error(), "limit is") {
		t.Fatalf("oversize: got %v, want a size error", err)
	}
}

func TestParseRejectsUnsupportedYAML(t *testing.T) {
	for _, doc := range []string{
		"a: !custom 1\n",
		"a: !!binary aGk=\n",
		"a: !!set {x: null}\n",
		"? [a, b]\n: 1\n",
		"a: 1\n---\nb: 2\n",
		"",
		"# only a comment\n",
	} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("unsupported %q: got nil error", doc)
		}
	}
}
