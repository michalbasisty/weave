package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A restart keeps the URL people have open; --new-token rotates it, and a malformed
// file is replaced instead of trusted.
func TestServeTokenIsStable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".weave", "serve.token")

	first := serveToken(root, false)
	if !validToken(first) {
		t.Fatalf("first token %q", first)
	}
	if again := serveToken(root, false); again != first {
		t.Fatalf("restart changed the token: %q -> %q", first, again)
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(raw)) != first {
		t.Fatalf("token file: %q %v", raw, err)
	}

	rotated := serveToken(root, true)
	if !validToken(rotated) || rotated == first {
		t.Fatalf("rotate: %q", rotated)
	}

	if err := os.WriteFile(path, []byte("not-a-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	replaced := serveToken(root, false)
	if !validToken(replaced) || replaced == rotated {
		t.Fatalf("malformed file was trusted: %q", replaced)
	}
}

// A second serve must not print a link to the first one's port: without --port it
// moves to the next free port, with --port it refuses.
func TestServeMovesOffABusyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	ln, err := listenFrom(port, true)
	if err != nil {
		t.Fatalf("default port busy: %v", err)
	}
	if got := ln.Addr().(*net.TCPAddr).Port; got == port {
		t.Fatalf("bound the busy port %d", got)
	}
	ln.Close()

	if ln, err := listenFrom(port, false); err == nil {
		ln.Close()
		t.Fatal("explicit --port on a busy port should fail")
	} else if !strings.Contains(err.Error(), "taken") {
		t.Fatalf("error: %v", err)
	}
}
