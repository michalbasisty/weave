//go:build unix

package rootio

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// watchdog runs fn and fails the test if it blocks, so a FIFO open that loses
// its non-blocking flag is reported instead of hanging the suite.
func watchdog(t *testing.T, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("operation blocked on a FIFO")
		return nil
	}
}

func TestFIFORefusedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}

	var opened *os.File
	err := watchdog(t, func() error {
		f, err := OpenRegular(r, "pipe", os.O_RDONLY, 0)
		opened = f
		return err
	})
	if err == nil {
		_ = opened.Close()
		t.Fatal("OpenRegular accepted a FIFO")
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("OpenRegular err = %v, want ErrNotRegular", err)
	}

	if err := watchdog(t, func() error {
		return WriteFile(r, "pipe", []byte("pwned"), 0o600)
	}); err == nil {
		t.Fatal("WriteFile accepted a FIFO")
	}
	if err := watchdog(t, func() error {
		return AppendFile(r, "pipe", []byte("pwned"), 0o600)
	}); err == nil {
		t.Fatal("AppendFile accepted a FIFO")
	}

	info, err := os.Lstat(pipe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO replaced by %v", info.Mode())
	}
}
