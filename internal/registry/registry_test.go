package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Produced by running src/registry.ts table() via node (Node 24.16.0).
const goldenTable = "name     size                                path\n" +
	"weave     120 files ·   0 ep · 26 changes    C:/work/weave\n" +
	"QR-gent    12 files ·   3 ep ·  1 changes    C:/work/QR-gent\n" +
	"aaa      not indexed                         /tmp/aaa"

func useHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("WEAVE_HOME", home)
	return home
}

func TestTouchNamesAndDedup(t *testing.T) {
	home := useHome(t)
	if got := HomeDir(); got != home {
		t.Fatalf("HomeDir = %q, want %q", got, home)
	}
	first := Touch(`C:\work\Alpha\`)
	if first.Name != "Alpha" || first.Root != `C:\work\Alpha\` || first.Files != nil {
		t.Fatalf("first touch: %+v", first)
	}
	same := Touch("c:/WORK/alpha")
	if same.Name != "Alpha" {
		t.Errorf("same root got name %q", same.Name)
	}
	if got := Load(); len(got) != 1 {
		t.Fatalf("same root made %d entries", len(got))
	}
	if got := Touch("C:/other/Alpha"); got.Name != "Alpha-2" {
		t.Errorf("collision name %q, want Alpha-2", got.Name)
	}
	if got := Touch("C:/third/Alpha"); got.Name != "Alpha-3" {
		t.Errorf("second collision name %q, want Alpha-3", got.Name)
	}
	entries := Load()
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if len(entries[0].LastUsed) != 24 || !strings.HasSuffix(entries[0].LastUsed, "Z") {
		t.Errorf("lastUsed %q is not an ISO timestamp", entries[0].LastUsed)
	}
	if got := Touch("D:/"); got.Name != "project" {
		t.Errorf("drive root got name %q, want project", got.Name)
	}
}

func TestSaveCapAndShape(t *testing.T) {
	useHome(t)
	entries := make([]Entry, 0, 205)
	for i := 0; i < 205; i++ {
		entries = append(entries, Entry{Name: "p" + string(rune('a'+i%26)), Root: "/r", LastUsed: "t"})
	}
	Save(entries)
	got := Load()
	if len(got) != 200 {
		t.Fatalf("saved %d entries, want cap 200", len(got))
	}
	raw, err := os.ReadFile(filepath.Join(HomeDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "{\n  \"projects\": [\n    {\n      \"name\": ") {
		t.Errorf("not 2-space JSON: %q", text[:min(80, len(text))])
	}
	if !strings.HasSuffix(text, "\n") || !strings.HasSuffix(strings.TrimSuffix(text, "\n"), "}") {
		t.Errorf("missing trailing newline / closing brace: %q", text[len(text)-20:])
	}
	if strings.Contains(text, "files") {
		t.Error("unset counters must be omitted")
	}

	zero := 0
	Save([]Entry{{Name: "z", Root: "/z", LastUsed: "t", Files: &zero, Endpoints: &zero, Changes: &zero}})
	raw, err = os.ReadFile(filepath.Join(HomeDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	for _, want := range []string{`"files": 0`, `"endpoints": 0`, `"changes": 0`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s in %q", want, text)
		}
	}

	// Byte for byte what JSON.stringify({ projects }, null, 2) + "\n" writes.
	files, endpoints, changes := 12, 0, 5
	Save([]Entry{
		{Name: "Alpha", Root: "C:/work/Alpha", LastUsed: "2026-01-02T03:04:05.000Z", Files: &files, Endpoints: &endpoints, Changes: &changes},
		{Name: "bare", Root: "C:/work/bare", LastUsed: "2026-01-02T03:04:05.000Z"},
	})
	raw, err = os.ReadFile(filepath.Join(HomeDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{
  "projects": [
    {
      "name": "Alpha",
      "root": "C:/work/Alpha",
      "lastUsed": "2026-01-02T03:04:05.000Z",
      "files": 12,
      "endpoints": 0,
      "changes": 5
    },
    {
      "name": "bare",
      "root": "C:/work/bare",
      "lastUsed": "2026-01-02T03:04:05.000Z"
    }
  ]
}
`
	if got := string(raw); got != wantJSON {
		t.Errorf("saved JSON:\ngot  %q\nwant %q", got, wantJSON)
	}
}

func TestNoteIndex(t *testing.T) {
	useHome(t)
	Touch("C:/work/weave")
	NoteIndex("c:\\WORK\\weave", Counts{Files: 12, Endpoints: 3, Changes: 1})
	entries := Load()
	if len(entries) != 1 || entries[0].Files == nil || *entries[0].Files != 12 || *entries[0].Endpoints != 3 || *entries[0].Changes != 1 {
		t.Fatalf("after noteIndex: %+v", entries)
	}
	NoteIndex("C:/unknown", Counts{Files: 99})
	if got := Load(); len(got) != 1 || *got[0].Files != 12 {
		t.Fatalf("unknown root changed the registry: %+v", got)
	}
}

func TestCandidatesResolveAndForget(t *testing.T) {
	useHome(t)
	for _, root := range []string{"C:/work/QR-gent", "C:/work/QR-gent-print", "C:/work/alpha-one", "C:/work/alpha-two"} {
		Touch(root)
	}
	if got := Candidates("qr"); len(got) != 2 {
		t.Errorf("Candidates(qr) = %d, want 2", len(got))
	}
	if got := Candidates("print"); len(got) != 1 || got[0].Name != "QR-gent-print" {
		t.Errorf("Candidates(print) = %+v", got)
	}
	if got := Candidates(""); len(got) != 0 {
		t.Errorf("Candidates(empty) = %+v", got)
	}
	cases := []struct {
		in   string
		want string
	}{
		{"qrgent", "QR-gent"},
		{"print", "QR-gent-print"},
		{"alphaone", "alpha-one"},
		{"qr", ""},
		{"alpha", ""},
		{"nope", ""},
	}
	for _, c := range cases {
		got := Resolve(c.in)
		if c.want == "" {
			if got != nil {
				t.Errorf("Resolve(%q) = %q, want nil", c.in, got.Name)
			}
			continue
		}
		if got == nil || got.Name != c.want {
			t.Errorf("Resolve(%q) = %+v, want %q", c.in, got, c.want)
		}
	}
	if !Forget("QR-gent") {
		t.Error("Forget known name returned false")
	}
	if Forget("QR-gent") {
		t.Error("Forget twice returned true")
	}
	if got := Load(); len(got) != 3 {
		t.Errorf("after forget: %d entries", len(got))
	}
}

func TestTableMatchesTypeScript(t *testing.T) {
	zero, three, one, twentySix := 0, 3, 1, 26
	twelve, oneTwenty := 12, 120
	entries := []Entry{
		{Name: "QR-gent", Root: "C:/work/QR-gent", LastUsed: "2026-01-02T00:00:00.000Z", Files: &twelve, Endpoints: &three, Changes: &one},
		{Name: "weave", Root: "C:/work/weave", LastUsed: "2026-01-03T00:00:00.000Z", Files: &oneTwenty, Endpoints: &zero, Changes: &twentySix},
		{Name: "aaa", Root: "/tmp/aaa", LastUsed: "2025-12-31T00:00:00.000Z"},
	}
	if got := Table(entries); got != goldenTable {
		t.Errorf("table:\ngot  %q\nwant %q", got, goldenTable)
	}
	if got, want := Table(nil), "no projects yet — run `weave` inside a repo"; got != want {
		t.Errorf("empty table: got %q, want %q", got, want)
	}
	if entries[0].Name != "QR-gent" || entries[2].Name != "aaa" {
		t.Error("Table reordered its argument")
	}
}

func TestLoadToleranceAndExists(t *testing.T) {
	home := useHome(t)
	if got := Load(); len(got) != 0 {
		t.Fatalf("missing registry: %+v", got)
	}
	path := filepath.Join(home, "projects.json")
	for _, text := range []string{"not json", `{"projects":{}}`, `{"projects":null}`, `[]`} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Load(); len(got) != 0 {
			t.Errorf("Load(%q) = %+v, want empty", text, got)
		}
	}
	if err := os.WriteFile(path, []byte(`{"projects":[{"name":"x"},{"name":"y","files":2}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if len(got) != 2 || got[0].Name != "x" || got[0].Files != nil || got[1].Files == nil || *got[1].Files != 2 {
		t.Errorf("Load partial entries: %+v", got)
	}
	if !Exists(home) || Exists(filepath.Join(home, "absent")) {
		t.Error("Exists is wrong")
	}
}
