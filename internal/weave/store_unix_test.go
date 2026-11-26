//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package weave

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A FIFO named graph.json must not be waited on by either a read or a write: the
// pinned root's non-blocking open plus the handle check refuse it at once, and the
// FIFO is left as a FIFO.
func TestUnixGraphFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	weave := filepath.Join(root, ".weave")
	if err := os.MkdirAll(weave, 0o755); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(weave, "graph.json")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	if _, err := noBlockValue(t, "LoadGraph on a FIFO", func() (*Graph, error) {
		return LoadGraph(root)
	}); err == nil {
		t.Error("LoadGraph accepted a FIFO")
	}
	if g, _ := noBlockValue(t, "LoadIfPresent on a FIFO", func() (*Graph, error) {
		return LoadIfPresent(root), nil
	}); g != nil {
		t.Error("LoadIfPresent accepted a FIFO")
	}
	if _, err := noBlockValue(t, "SaveGraph over a FIFO", func() (string, error) {
		return SaveGraph(root, &Graph{})
	}); err == nil {
		t.Error("SaveGraph accepted a FIFO")
	}

	info, err := os.Lstat(pipe)
	if err != nil {
		t.Fatalf("stat graph.json after the refused calls: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("graph.json is no longer a FIFO: %v", info.Mode())
	}
}

// A graph.json linked outside the repo is not read: the pinned root refuses the
// final link rather than loading the outside file's bytes.
func TestUnixGraphDoesNotReadLinkedIndex(t *testing.T) {
	root := t.TempDir()
	weave := filepath.Join(root, ".weave")
	if err := os.MkdirAll(weave, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "graph.json")
	const body = `{"version":3,"root":"outside","builtAt":"b","fileCount":0,"nodes":[],"edges":[],"prevEndpoints":[]}`
	if err := os.WriteFile(secret, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(weave, "graph.json")); err != nil {
		t.Fatalf("create a file symlink, which this Unix test requires: %v", err)
	}

	if _, err := LoadGraph(root); err == nil {
		t.Fatal("LoadGraph read a linked index outside the repo")
	}
	if LoadIfPresent(root) != nil {
		t.Error("LoadIfPresent read a linked index outside the repo")
	}
	if raw, err := os.ReadFile(secret); err != nil || string(raw) != body {
		t.Errorf("the link's target changed: %q, %v", raw, err)
	}
}
