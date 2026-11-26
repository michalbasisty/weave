//go:build unix

package usage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadRefusesFIFOWithoutBlocking makes sure the non-blocking, regular-file
// checked read does not wait for a writer on a planted FIFO.
func TestReadRefusesFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(root, ".weave", "usage.jsonl")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	done := make(chan []Entry, 1)
	go func() { done <- Read(root) }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Fatalf("read FIFO: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read blocked on a FIFO")
	}
}
