//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package weave

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A FIFO named memory.jsonl must not block the duplicate check, recall or the
// maintenance calls: every read and write opens it non-blocking through the pinned
// root and refuses the handle before anything waits.
func TestUnixMemoryFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	weave := filepath.Join(root, ".weave")
	if err := os.MkdirAll(weave, 0o755); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(weave, "memory.jsonl")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	if notes, err := noBlockValue(t, "ReadNotes on a FIFO", func() ([]Note, error) {
		return ReadNotes(root)
	}); err == nil {
		t.Errorf("ReadNotes accepted a FIFO: %v", notes)
	}
	if _, err := noBlockValue(t, "RecallText on a FIFO", func() (string, error) {
		return RecallText(root, nil)
	}); err == nil {
		t.Error("RecallText accepted a FIFO")
	}
	if n, err := noBlockValue(t, "Remember over a FIFO", func() (*Note, error) {
		return Remember(root, []string{"a.ts"}, "Invoices are immutable once sent", "2026-01-01T00:00:00.000Z")
	}); err == nil || n != nil {
		t.Errorf("Remember accepted a FIFO: n=%v err=%v", n, err)
	}
	if _, err := noBlockValue(t, "ClearMemory over a FIFO", func() (int, error) {
		return ClearMemory(root)
	}); err == nil {
		t.Error("ClearMemory accepted a FIFO")
	}
	if _, err := noBlockValue(t, "PruneMemory over a FIFO", func() (int, error) {
		return PruneMemory(root)
	}); err == nil {
		t.Error("PruneMemory accepted a FIFO")
	}

	info, err := os.Lstat(pipe)
	if err != nil {
		t.Fatalf("stat memory.jsonl after the refused calls: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("memory.jsonl is no longer a FIFO: %v", info.Mode())
	}
}

// A memory.jsonl linked outside the repo is not read, so the duplicate check cannot
// be fed by an outside note. Reading it would suppress the append and look like a
// successful duplicate; the confined read leaves the append to fail on the link.
func TestUnixMemoryDoesNotReadLinkedFile(t *testing.T) {
	root := t.TempDir()
	weave := filepath.Join(root, ".weave")
	if err := os.MkdirAll(weave, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "memory.jsonl")
	const noteText = "Invoices are immutable once sent"
	body := `{"at":"2026-01-01T00:00:00.000Z","files":["a.ts"],"note":"` + noteText + `"}` + "\n"
	if err := os.WriteFile(secret, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(weave, "memory.jsonl")); err != nil {
		t.Fatalf("create a file symlink, which this Unix test requires: %v", err)
	}

	if notes, err := ReadNotes(root); err == nil {
		t.Fatalf("ReadNotes read a linked memory file: %v", notes)
	}
	if n, err := Remember(root, []string{"a.ts"}, noteText, "2026-01-02T00:00:00.000Z"); err == nil || n != nil {
		t.Fatalf("Remember treated the outside note as a duplicate: n=%v err=%v", n, err)
	}
	if raw, err := os.ReadFile(secret); err != nil || string(raw) != body {
		t.Errorf("the link's target changed: %q, %v", raw, err)
	}
}
