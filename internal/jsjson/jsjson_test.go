package jsjson

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/michalbasisty/weave/internal/jsstr"
)

// The Phase 1 gate: a graph written by the TypeScript weave re-encodes byte for byte.
func TestGraphFilesRoundTrip(t *testing.T) {
	for _, p := range []string{"../../test/fixture/.weave/graph.json", "../../.weave/graph.json"} {
		t.Run(p, func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if os.IsNotExist(err) {
				t.Skipf("%s missing: run `node src/cli.ts index` first", p)
			}
			if err != nil {
				t.Fatal(err)
			}
			v, err := Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if out := Marshal(v); !bytes.Equal(out, raw) {
				t.Fatalf("re-encoded %d bytes differ from %d original bytes at %d", len(out), len(raw), firstDiff(out, raw))
			}
			t.Logf("%d bytes identical", len(raw))
		})
	}
}

func TestCraftedObject(t *testing.T) {
	in := "{ \"z\": {\"b\": [1, -0.5, 2e-7, {\"y\": null, \"x\": true}], \"a\": \"\\u0001\\u001f\\u007f\\b\\f\\n\\r\\t\\\"\\\\\\/\"},\n" +
		"  \"html\": \"\\u003cscript\\u003e &amp;\", \"ls\": \"\\u2028\\u2029\", \"lone\": \"\\ud800x\\udfff\", \"pair\": \"\\ud83d\\ude00\",\n" +
		"  \"5\": \"index keys first\", \"a\": \"first\", \"a\": \"last value, first place\" }"
	want := "{\"5\":\"index keys first\",\"z\":{\"b\":[1,-0.5,2e-7,{\"y\":null,\"x\":true}],\"a\":\"\\u0001\\u001f\x7f\\b\\f\\n\\r\\t\\\"\\\\/\"}," +
		"\"html\":\"<script> &amp;\",\"ls\":\"\xe2\x80\xa8\xe2\x80\xa9\",\"lone\":\"\\ud800x\\udfff\",\"pair\":\"\xf0\x9f\x98\x80\"," +
		"\"a\":\"last value, first place\"}"
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(Marshal(v)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := string(Marshal(mustParse(t, want))); got != want {
		t.Fatalf("second pass changed the bytes: %s", got)
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{"", "{", "[1,]", "{\"a\":1,}", "01", "1.", "-", "\"\x01\"", "\"\\x\"", "nul", "1 2", "\xef\xbb\xbf{}", "{'a':1}"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) succeeded, JSON.parse throws", in)
		}
	}
}

func TestObjectSetOrder(t *testing.T) {
	o := &Object{}
	for _, k := range []string{"b", "10", "a", "2", "01", "4294967295", "4294967294", "b"} {
		o.Set(k, Int(len(k)))
	}
	got := string(Marshal(Obj(o)))
	want := `{"2":1,"10":2,"4294967294":10,"b":1,"a":1,"01":2,"4294967295":10}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	c := o.Clone()
	c.Set("suspect", Bool(true))
	c.Delete("a")
	if o.Len() != 7 || c.Keys()[c.Len()-1] != "suspect" || c.Has("a") {
		t.Fatalf("clone shares state or appends wrongly: %v / %v", o.Keys(), c.Keys())
	}
}

type oracle struct {
	Strings []struct {
		In  []uint16 `json:"in"`
		Out string   `json:"out"`
	} `json:"strings"`
	Numbers []struct {
		Bits string `json:"bits"`
		Out  string `json:"out"`
	} `json:"numbers"`
	Parsed []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"parsed"`
}

func loadOracle(t *testing.T) oracle {
	raw, err := os.ReadFile("testdata/oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var o oracle
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestStringsMatchNode(t *testing.T) {
	for _, c := range loadOracle(t).Strings {
		got := AppendString(nil, jsstr.FromUnits(c.In))
		if string(got) != c.Out {
			t.Errorf("units %x: got %s, Node %s", c.In, got, c.Out)
		}
		back, err := Parse(got)
		if err != nil || jsstr.Len(back.Str()) != len(c.In) {
			t.Errorf("units %x: re-parse %q, %v", c.In, back.Str(), err)
		}
	}
}

func TestNumbersMatchNode(t *testing.T) {
	for _, c := range loadOracle(t).Numbers {
		bits, err := strconv.ParseUint(c.Bits, 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		f := math.Float64frombits(bits)
		if got := string(Marshal(Num(f))); got != c.Out {
			t.Errorf("%s (%v): got %s, Node %s", c.Bits, f, got, c.Out)
		}
	}
}

func TestParsedMatchNode(t *testing.T) {
	for _, c := range loadOracle(t).Parsed {
		if got := string(Marshal(mustParse(t, c.In))); got != c.Out {
			t.Errorf("%s: got %s, Node %s", c.In, got, c.Out)
		}
	}
}

func mustParse(t *testing.T, s string) Value {
	t.Helper()
	v, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
