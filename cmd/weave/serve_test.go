package main

import (
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
