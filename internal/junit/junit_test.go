package junit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const suiteXML = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="calc">
    <testcase classname="Calc" name="adds REQ-1" file="src/calc.test.ts" time="0.01"/>
    <testcase name="fails FR-2" classname="Calc">
      <failure message="wrong sum">expected 4</failure>
    </testcase>
    <testcase name="skips NFR-3" suite="Legacy"><skipped/></testcase>
    <testcase name="errors" classname="Calc"><error message="boom"/></testcase>
    <testcase name="a &amp; b &lt;c&gt;" classname="Suite &quot;Q&quot;"/>
    <testcase name="num &#65; unknown &nbsp; bare & ok &amp;lt;" classname="Esc"/>
  </testsuite>
</testsuites>`

func TestParseStatuses(t *testing.T) {
	cases := Parse(suiteXML)
	if len(cases) != 6 {
		t.Fatalf("parsed %d cases, want 6: %+v", len(cases), cases)
	}
	want := []struct {
		name, suite string
		status      Status
	}{
		{"adds REQ-1", "Calc", Pass},
		{"fails FR-2", "Calc", Fail},
		{"skips NFR-3", "Legacy", Skip},
		{"errors", "Calc", Fail},
		{`a & b <c>`, `Suite "Q"`, Pass},
		{`num &#65; unknown &nbsp; bare & ok &lt;`, "Esc", Pass},
	}
	for i, w := range want {
		if cases[i].Name != w.name || cases[i].Suite != w.suite || cases[i].Status != w.status {
			t.Errorf("case %d = %+v, want %+v", i, cases[i], w)
		}
	}
	if cases[0].File == nil || *cases[0].File != "src/calc.test.ts" {
		t.Errorf("file attr: %+v", cases[0].File)
	}
	if cases[1].File != nil {
		t.Errorf("missing file attr became %q", *cases[1].File)
	}
}

func TestParseTolerance(t *testing.T) {
	if got := Parse(""); len(got) != 0 {
		t.Errorf("empty XML parsed %+v", got)
	}
	if got := Parse("not xml at all"); len(got) != 0 {
		t.Errorf("plain text parsed %+v", got)
	}
	got := Parse(`<testsuite><testcase name="ok"/></testsuite>`)
	if len(got) != 1 || got[0].Status != Pass {
		t.Errorf("self-closing case: %+v", got)
	}
	// A broken tail keeps the cases already read, as the regex does.
	got = Parse(`<testsuite><testcase name="first"/><testcase name="second" classname="S">`)
	if len(got) != 2 || got[1].Name != "second" {
		t.Errorf("broken document: %+v", got)
	}
	// A bare ampersand is invalid XML but the TypeScript regex accepts it.
	got = Parse(`<testsuite><testcase name="a & b"/></testsuite>`)
	if len(got) != 1 || got[0].Name != "a & b" {
		t.Errorf("bare ampersand: %+v", got)
	}
}

func TestFindJUnitFile(t *testing.T) {
	root := t.TempDir()
	if p, ok := FindJUnitFile(root); ok || p != "" {
		t.Fatalf("missing report found %q", p)
	}
	weave := filepath.Join(root, ".weave", "junit.xml")
	plain := filepath.Join(root, "junit.xml")
	for _, p := range []string{weave, plain} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("<testsuites/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-time.Hour)
	if err := os.Chtimes(weave, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(plain, newer, newer); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindJUnitFile(root); !ok || got != plain {
		t.Errorf("newest = %q (%v), want %q", got, ok, plain)
	}
	now := time.Now()
	if err := os.Chtimes(weave, now, now); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindJUnitFile(root); !ok || got != weave {
		t.Errorf("newest = %q (%v), want %q", got, ok, weave)
	}
}

func TestReqsInCase(t *testing.T) {
	c := Case{Name: "adds REQ-1 and NFR-22, not FR-3x or BAD-4", Suite: "FR-3-ABC"}
	got := ReqsInCase(c)
	want := []string{"REQ-1", "NFR-22", "FR-3-ABC"}
	if len(got) != len(want) {
		t.Fatalf("reqs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reqs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := ReqsInCase(Case{Name: "plain", Suite: "suite"}); len(got) != 0 {
		t.Errorf("plain case found %v", got)
	}
}
