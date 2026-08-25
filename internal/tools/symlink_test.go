package tools_test

import (
	"os"
	"path/filepath"
	"testing"
)

// T-CONFINEMENT-SYMLINKED-ROOT-ANCESTOR, remote half: the remote base
// itself sits behind a symlinked ancestor directory; an ordinary push and
// sync still succeed, because confinement compares against the canonical
// remote base, not the uncanonicalized one.
func TestPushSync_SymlinkedRemoteAncestorSucceeds(t *testing.T) {
	e := newEnv(t, "link/api")

	real := filepath.Join(e.server.Root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(e.server.Root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	e.writeLocal("main.go", "package main")

	res, out := e.call("push", map[string]any{
		"vm":    "testvm",
		"files": []string{"main.go"},
	})
	if res.IsError {
		t.Fatalf("push through a symlinked remote ancestor should succeed, got: %v", out)
	}
	if _, err := os.Stat(filepath.Join(real, "api", "main.go")); err != nil {
		t.Fatalf("expected the file under the real (post-symlink) directory: %v", err)
	}

	res2, out2 := e.call("sync", map[string]any{"vm": "testvm"})
	if res2.IsError {
		t.Fatalf("sync through a symlinked remote ancestor should succeed, got: %v", out2)
	}
}
