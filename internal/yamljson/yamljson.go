// Package yamljson reads YAML into the same ordered values as jsjson, so a spec
// committed in either format reaches the extractors as the same tree.
//
// Mapping order is preserved the way jsjson preserves it: insertion order, with
// JavaScript's integer-like keys first. Duplicate keys are rejected instead of
// letting the last one win, because a spec that repeats a path or a status code
// is ambiguous and should be reported rather than guessed at.
//
// Parsing is bounded. Input size, nesting depth, expanded node count and alias
// depth all have limits, and aliases are expanded in place so a YAML bomb fails
// as an error instead of consuming memory. Merge keys are resolved with YAML's
// precedence (explicit keys win, earlier maps win over later ones). Tags outside
// the JSON data model (binary, custom tags) are errors: an OpenAPI document needs
// none of them, and silently dropping one would change the contract.
package yamljson

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"go.yaml.in/yaml/v3"
)

// MaxBytes bounds the document before the YAML parser sees it.
const MaxBytes = 4 << 20

// limits are the bounds a parse runs under. Tests use smaller values to reach
// each boundary without building a multi-megabyte document.
type limits struct {
	depth      int // nesting of mappings, sequences and alias expansion
	nodes      int // expanded values, including every alias copy
	aliasDepth int // alias chains, so a cycle is reported early
}

var defaultLimits = limits{depth: 200, nodes: 2 << 20, aliasDepth: 100}

// Parse reads one YAML document into a jsjson.Value under the default limits.
// An empty stream, a second document, and any YAML feature outside the JSON
// data model return an error.
func Parse(data []byte) (jsjson.Value, error) {
	return parse(data, defaultLimits)
}

type converter struct {
	lim       limits
	nodes     int
	aliasSeen map[*yaml.Node]bool
}

func parse(data []byte, lim limits) (v jsjson.Value, err error) {
	if len(data) > MaxBytes {
		return jsjson.Value{}, fmt.Errorf("yamljson: document is %d bytes, limit is %d", len(data), MaxBytes)
	}
	// The yaml package reports some invalid tags by panicking through failf;
	// Decode turns its own panics into errors, but ShortTag during conversion
	// can still reach that path.
	defer func() {
		if r := recover(); r != nil {
			v, err = jsjson.Value{}, fmt.Errorf("yamljson: %v", r)
		}
	}()

	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return jsjson.Value{}, fmt.Errorf("yamljson: %w", err)
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case err == nil:
		return jsjson.Value{}, errors.New("yamljson: multiple documents are not supported")
	case !errors.Is(err, io.EOF):
		return jsjson.Value{}, fmt.Errorf("yamljson: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return jsjson.Value{}, errors.New("yamljson: document has no root value")
	}
	c := &converter{lim: lim, aliasSeen: map[*yaml.Node]bool{}}
	return c.value(doc.Content[0], 0)
}

func (c *converter) value(n *yaml.Node, depth int) (jsjson.Value, error) {
	if depth > c.lim.depth {
		return jsjson.Value{}, fmt.Errorf("yamljson: nesting deeper than %d", c.lim.depth)
	}
	c.nodes++
	if c.nodes > c.lim.nodes {
		return jsjson.Value{}, fmt.Errorf("yamljson: expanded document larger than %d nodes", c.lim.nodes)
	}
	switch n.Kind {
	case yaml.AliasNode:
		if n.Alias == nil {
			return jsjson.Value{}, fmt.Errorf("yamljson: alias %q has no target", n.Value)
		}
		if c.aliasSeen[n] {
			return jsjson.Value{}, fmt.Errorf("yamljson: alias %q contains itself", n.Value)
		}
		if len(c.aliasSeen) >= c.lim.aliasDepth {
			return jsjson.Value{}, fmt.Errorf("yamljson: alias chain deeper than %d", c.lim.aliasDepth)
		}
		c.aliasSeen[n] = true
		v, err := c.value(n.Alias, depth+1)
		delete(c.aliasSeen, n)
		return v, err
	case yaml.ScalarNode:
		return c.scalar(n)
	case yaml.SequenceNode:
		if tag := n.ShortTag(); tag != "!!seq" {
			return jsjson.Value{}, fmt.Errorf("yamljson: unsupported tag %q on a sequence", tag)
		}
		vs := make([]jsjson.Value, 0, len(n.Content))
		for _, e := range n.Content {
			v, err := c.value(e, depth+1)
			if err != nil {
				return jsjson.Value{}, err
			}
			vs = append(vs, v)
		}
		return jsjson.Arr(vs...), nil
	case yaml.MappingNode:
		if tag := n.ShortTag(); tag != "!!map" {
			return jsjson.Value{}, fmt.Errorf("yamljson: unsupported tag %q on a mapping", tag)
		}
		return c.mapping(n, depth)
	default:
		return jsjson.Value{}, fmt.Errorf("yamljson: unsupported node kind %d", n.Kind)
	}
}

// mapping converts a mapping. Explicit keys are inserted first, then merge keys
// in source order, with a key that already exists left alone; this is the
// precedence YAML gives a merge, independent of where `<<` appears.
func (c *converter) mapping(n *yaml.Node, depth int) (jsjson.Value, error) {
	if len(n.Content)%2 != 0 {
		return jsjson.Value{}, errors.New("yamljson: malformed mapping")
	}
	o := &jsjson.Object{}
	seen := make(map[string]struct{}, len(n.Content)/2)
	var merges []*yaml.Node
	for i := 0; i < len(n.Content); i += 2 {
		kn := n.Content[i]
		if isMergeKey(kn) {
			merges = append(merges, n.Content[i+1])
			continue
		}
		k, err := c.key(kn, depth+1)
		if err != nil {
			return jsjson.Value{}, err
		}
		if _, dup := seen[k]; dup {
			return jsjson.Value{}, fmt.Errorf("yamljson: duplicate key %q", k)
		}
		seen[k] = struct{}{}
		v, err := c.value(n.Content[i+1], depth+1)
		if err != nil {
			return jsjson.Value{}, err
		}
		o.Set(k, v)
	}
	for _, m := range merges {
		v, err := c.value(m, depth+1)
		if err != nil {
			return jsjson.Value{}, err
		}
		if err := mergeInto(o, seen, v); err != nil {
			return jsjson.Value{}, err
		}
	}
	return jsjson.Obj(o), nil
}

func mergeInto(o *jsjson.Object, seen map[string]struct{}, v jsjson.Value) error {
	switch v.Kind() {
	case jsjson.KindObject:
		mo := v.Object()
		for i := 0; i < mo.Len(); i++ {
			k, x := mo.At(i)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			o.Set(k, x)
		}
	case jsjson.KindArray:
		for _, x := range v.Array() {
			if err := mergeInto(o, seen, x); err != nil {
				return err
			}
		}
	default:
		return errors.New("yamljson: merge value must be a mapping or a sequence of mappings")
	}
	return nil
}

func isMergeKey(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!merge"
}

// key turns a scalar key into the property name JSON would give it. Collection
// and merge keys have no JSON equivalent.
func (c *converter) key(n *yaml.Node, depth int) (string, error) {
	if depth > c.lim.depth {
		return "", fmt.Errorf("yamljson: nesting deeper than %d", c.lim.depth)
	}
	if n.Kind != yaml.ScalarNode {
		return "", errors.New("yamljson: mapping key must be a scalar")
	}
	if n.ShortTag() == "!!merge" {
		return "", errors.New("yamljson: merge keys are not supported")
	}
	v, err := c.scalar(n)
	if err != nil {
		return "", err
	}
	switch v.Kind() {
	case jsjson.KindString:
		return v.Str(), nil
	case jsjson.KindBool:
		if v.Bool() {
			return "true", nil
		}
		return "false", nil
	case jsjson.KindNumber:
		return v.Lexeme(), nil
	}
	return "", errors.New("yamljson: mapping key has no JSON name")
}

func (c *converter) scalar(n *yaml.Node) (jsjson.Value, error) {
	switch tag := n.ShortTag(); tag {
	case "!!str", "":
		return jsjson.Str(n.Value), nil
	case "!!null":
		return jsjson.Null(), nil
	case "!!bool":
		switch n.Value {
		case "true", "True", "TRUE":
			return jsjson.Bool(true), nil
		case "false", "False", "FALSE":
			return jsjson.Bool(false), nil
		}
		return jsjson.Value{}, fmt.Errorf("yamljson: invalid bool %q", n.Value)
	case "!!int":
		if v, ok := parseInt(n.Value); ok {
			return v, nil
		}
		return jsjson.Value{}, fmt.Errorf("yamljson: invalid integer %q", n.Value)
	case "!!float":
		if f, ok := parseFloat(n.Value); ok {
			return jsjson.Num(f), nil // NaN and ±Inf become null, as JSON.stringify does
		}
		return jsjson.Value{}, fmt.Errorf("yamljson: invalid float %q", n.Value)
	case "!!timestamp":
		// JSON has no date type; a timestamp in a spec is just its text.
		return jsjson.Str(n.Value), nil
	case "!!merge":
		return jsjson.Value{}, errors.New("yamljson: merge keys are not supported")
	case "!!binary":
		return jsjson.Value{}, errors.New("yamljson: binary scalars are not supported")
	default:
		return jsjson.Value{}, fmt.Errorf("yamljson: unsupported tag %q", tag)
	}
}

// parseInt accepts the integer forms the YAML resolver accepts and returns a
// decimal JSON number, keeping the value exact for the full uint64 range.
func parseInt(s string) (jsjson.Value, bool) {
	t := strings.ReplaceAll(s, "_", "")
	if t == "" {
		return jsjson.Value{}, false
	}
	if n, err := strconv.ParseInt(t, 0, 64); err == nil {
		return number(strconv.FormatInt(n, 10)), true
	}
	if n, err := strconv.ParseUint(t, 0, 64); err == nil {
		return number(strconv.FormatUint(n, 10)), true
	}
	return jsjson.Value{}, false
}

// number builds a number Value from a decimal lexeme. The parse cannot fail for
// the strings built here; a failure would mean a bug, so it yields null rather
// than panicking in the middle of a spec.
func number(lex string) jsjson.Value {
	v, err := jsjson.Parse([]byte(lex))
	if err != nil {
		return jsjson.Null()
	}
	return v
}

func parseFloat(s string) (float64, bool) {
	t := strings.ReplaceAll(s, "_", "")
	switch t {
	case ".nan", ".NaN", ".NAN":
		return math.NaN(), true
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return math.Inf(1), true
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1), true
	}
	f, err := strconv.ParseFloat(t, 64)
	return f, err == nil
}
