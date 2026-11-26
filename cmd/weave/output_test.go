package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// linkDir points link at target. os.Symlink needs a privilege on Windows that a
// normal checkout lacks, so fall back to a directory junction there; os.Root refuses
// both when target is outside the pinned root.
func linkDir(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err == nil {
			return
		} else {
			t.Skipf("directory links unavailable: %v: %s", err, out)
		}
	}
	t.Skip("directory links unavailable")
}

// withGlobals keeps the package globals a command reads (root and os.Args) local to
// one test, so a test never depends on the checkout it runs in.
func withGlobals(t *testing.T, repo string, args ...string) {
	t.Helper()
	oldRoot, oldArgs := root, os.Args
	root = repo
	os.Args = append([]string{"weave"}, args...)
	t.Cleanup(func() {
		root = oldRoot
		os.Args = oldArgs
	})
}

// The default site belongs to the repository and is written through a pinned
// .weave/wiki; an explicit --out is still an arbitrary directory.
func TestWikiDefaultStaysInRepoAndExplicitOutIsArbitrary(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo, "wiki")
	if code := cmdWiki(); code != 0 {
		t.Fatalf("default wiki exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(repo, ".weave", "wiki", "index.html")); err != nil {
		t.Fatalf("default wiki has no index inside the repo: %v", err)
	}

	outside := t.TempDir()
	site := filepath.Join(outside, "site")
	os.Args = []string{"weave", "wiki", "--out", site}
	if code := cmdWiki(); code != 0 {
		t.Fatalf("explicit wiki exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(site, "index.html")); err != nil {
		t.Fatalf("explicit --out did not write its site: %v", err)
	}
}

// A .weave/wiki swapped for a link out of the repo is refused, not followed.
func TestWikiDefaultRefusesEscapingDotWeave(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo, "wiki")
	if code := cmdWiki(); code != 0 {
		t.Fatalf("setup wiki exited %d", code)
	}

	local := filepath.Join(repo, ".weave", "wiki")
	if err := os.RemoveAll(local); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	linkDir(t, outside, local)

	if code := cmdWiki(); code != 1 {
		t.Fatalf("default wiki through an escaping link exited %d, want 1", code)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("default wiki wrote %d entr(ies) outside the repo", len(entries))
	}
}

// The default explore page is written inside the repo, and a target swapped for a
// link out of it is refused.
func TestExploreDefaultStaysInRepo(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo, "explore", "--no-open")
	if code := cmdExplore(); code != 0 {
		t.Fatalf("default explore exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(repo, ".weave", "explore.html")); err != nil {
		t.Fatalf("default explore page missing: %v", err)
	}
}

func TestExploreDefaultRefusesEscapingTarget(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo, "explore", "--no-open")
	if code := cmdExplore(); code != 0 {
		t.Fatalf("setup explore exited %d", code)
	}

	target := filepath.Join(repo, ".weave", "explore.html")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	linkDir(t, outside, target)

	if code := cmdExplore(); code != 1 {
		t.Fatalf("explore through an escaping link exited %d, want 1", code)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("explore wrote %d entr(ies) outside the repo", len(entries))
	}
}

// An index run refreshes an existing default wiki through the pinned directory.
func TestIndexRefreshesDefaultWikiInRepo(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo, "wiki")
	if code := cmdWiki(); code != 0 {
		t.Fatalf("setup wiki exited %d", code)
	}

	index := filepath.Join(repo, ".weave", "wiki", "index.html")
	if err := os.WriteFile(index, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"weave", "index"}
	if code := cmdIndex(); code != 0 {
		t.Fatalf("index exited %d", code)
	}
	raw, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "stale" || !strings.Contains(string(raw), "<!doctype") {
		t.Fatalf("index did not refresh the default wiki: %q", raw)
	}
}

func TestIndexRefreshRefusesEscapingDefaultWiki(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo, "index")
	if code := cmdIndex(); code != 0 {
		t.Fatalf("setup index exited %d", code)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "index.html"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".weave", "wiki")); err != nil {
		t.Fatal(err)
	}
	linkDir(t, outside, filepath.Join(repo, ".weave", "wiki"))

	if code := cmdIndex(); code != 0 {
		t.Fatalf("index exited %d", code)
	}
	raw, err := os.ReadFile(filepath.Join(outside, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "outside" {
		t.Fatalf("index refresh wrote outside the repo: %q", raw)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("index refresh added %d entr(ies) outside the repo", len(entries)-1)
	}
}

// The hook's ledger is written and read inside the repo, and a .weave directory
// swapped for a link out of it is neither written nor read.
func TestFiresStayInsideRepo(t *testing.T) {
	repo := t.TempDir()
	withGlobals(t, repo)

	appendFire(fire{At: "2026-01-01T00:00:00Z", Result: "pass", Ms: 1})
	got := readFires()
	if len(got) != 1 || got[0].Result != "pass" {
		t.Fatalf("fires: %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".weave", "fires.jsonl"))
	if err != nil || !strings.Contains(string(raw), `"result":"pass"`) {
		t.Fatalf("fire ledger: %q %v", raw, err)
	}
}

func TestFiresIgnoreEscapingDotWeave(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	ledger := filepath.Join(outside, "fires.jsonl")
	original := "{\"at\":\"2026-01-01T00:00:00Z\",\"result\":\"pass\",\"ms\":1}\n"
	if err := os.WriteFile(ledger, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir(t, outside, filepath.Join(repo, ".weave"))
	withGlobals(t, repo)

	appendFire(fire{At: "2026-01-02T00:00:00Z", Result: "fail", Ms: 2})
	if got := readFires(); len(got) != 0 {
		t.Fatalf("read %d fire(s) outside the repo", len(got))
	}
	raw, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != original {
		t.Fatalf("outside ledger changed: %q", raw)
	}
}

// A token outside the repo is neither trusted nor overwritten.
func TestServeTokenIgnoresEscapingDotWeave(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	planted := strings.Repeat("a", 32)
	tokenFile := filepath.Join(outside, "serve.token")
	if err := os.WriteFile(tokenFile, []byte(planted+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkDir(t, outside, filepath.Join(repo, ".weave"))

	got := serveToken(repo, false)
	if got == planted {
		t.Fatal("trusted a token outside the repo")
	}
	if !validToken(got) {
		t.Fatalf("token: %q", got)
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != planted {
		t.Fatalf("outside token changed: %q", raw)
	}
}
