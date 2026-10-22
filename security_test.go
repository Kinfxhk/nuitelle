package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllowedPathNoPrefixEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	sibling := filepath.Join(base, "root2")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "a.mp4")
	outside := filepath.Join(sibling, "b.mp4")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Roots: []string{root}}
	if !allowedPath(cfg, inside) {
		t.Fatal("inside file rejected")
	}
	if allowedPath(cfg, outside) {
		t.Fatal("sibling prefix escaped the root")
	}
	if allowedPath(cfg, filepath.Join(root, "..", "root2", "b.mp4")) {
		t.Fatal("traversal accepted")
	}
}
