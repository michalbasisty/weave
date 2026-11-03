package weave

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var reHref = regexp.MustCompile(`href="([^"]+)"`)

// The wiki has to be usable as documentation: every page says what the thing is,
// every link resolves on disk, and the whole set is stamped with its commit.
func TestWikiPagesResolveAndSayWhatTheyAre(t *testing.T) {
	root := copyFixture(t)
	g := BuildGraph(root)
	out := t.TempDir()

	res, err := WriteWiki(g, WikiOptions{OutDir: out, Commit: "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pages < 5 {
		t.Fatalf("only %d pages written", res.Pages)
	}

	index := readFile(t, filepath.Join(out, "index.html"))
	for _, want := range []string{"The contract gate passes", "Modules", "Endpoints", "Gaps in proof"} {
		if !strings.Contains(index, want) {
			t.Errorf("index is missing %q", want)
		}
	}

	ep := readFile(t, filepath.Join(out, "e", Slug("GET /api/v1/orders")+".html"))
	for _, want := range []string{"An HTTP endpoint", "handled in", "Proved by", "Source"} {
		if !strings.Contains(ep, want) {
			t.Errorf("endpoint page is missing %q:\n%s", want, ep)
		}
	}

	mod := readFile(t, filepath.Join(out, "m", "orders.html"))
	if !strings.Contains(mod, "Endpoints it handles") {
		t.Errorf("module page does not list endpoints")
	}

	if got := ReadWikiStamp(out); got != "0123456789abcdef" {
		t.Errorf("stamp: got %q", got)
	}

	// Every local link must point at a page that exists: a wiki with dead links is
	// worse than no wiki.
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				walk(p)
				continue
			}
			if !strings.HasSuffix(e.Name(), ".html") {
				continue
			}
			body := readFile(t, p)
			for _, m := range reHref.FindAllStringSubmatch(body, -1) {
				href := m[1]
				if strings.HasPrefix(href, "http") {
					continue
				}
				if i := strings.IndexByte(href, '#'); i >= 0 {
					if i == 0 {
						continue
					}
					href = href[:i]
				}
				target := filepath.Clean(filepath.Join(filepath.Dir(p), filepath.FromSlash(href)))
				if _, err := os.Stat(target); err != nil {
					t.Errorf("%s links to missing %s", strings.TrimPrefix(p, out), href)
				}
			}
		}
	}
	walk(out)
}

func TestSlugIsAStableFileName(t *testing.T) {
	cases := map[string]string{
		"GET /api/v1/tasks/:id":         "get-api-v1-tasks-id",
		"module:assignmentRules":        "module-assignmentrules",
		"page:/tasks/:id":               "page-tasks-id",
		"change:2026-10-27-phone-speed": "change-2026-10-27-phone-speed",
		"/":                             "page",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return string(b)
}
