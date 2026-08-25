package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckLocalLexical_TableDriven(t *testing.T) {
	root := "/home/me/work/api"
	cases := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{"plain file", "cmd/api/main.go", false},
		{"absolute path rejected", "/etc/passwd", true},
		{"dotdot segment rejected", "../outside", true},
		{"dotdot in the middle rejected", "cmd/../../outside", true},
		{"empty path rejected", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CheckLocalLexical(root, tc.rel)
			if tc.wantErr && !errors.Is(err, ErrTraversal) {
				t.Fatalf("expected ErrTraversal, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckRemoteLexical_TableDriven(t *testing.T) {
	base := "/srv/agents/projects/api"
	cases := []struct {
		name    string
		rel     string
		want    string
		wantErr bool
	}{
		{"plain dir", "subdir", base + "/subdir", false},
		{"dot means base itself", ".", base, false},
		{"absolute rejected", "/etc", "", true},
		{"dotdot rejected", "../outside", "", true},
		{"dotdot after descent rejected", "a/../../outside", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckRemoteLexical(base, tc.rel)
			if tc.wantErr {
				if !errors.Is(err, ErrTraversal) {
					t.Fatalf("expected ErrTraversal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// T-TRAVERSAL-PUSH-FILES-DOTDOT / T-TRAVERSAL-PUSH-FILES-ABSOLUTE via
// CheckLocalFile end-to-end.
func TestCheckLocalFile_RejectsDotDotAndAbsolute(t *testing.T) {
	dir := t.TempDir()
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../escape", "/etc/passwd"} {
		if _, err := CheckLocalFile(dir, canon, rel, filepath.EvalSymlinks); !errors.Is(err, ErrTraversal) {
			t.Fatalf("rel=%q: expected ErrTraversal, got %v", rel, err)
		}
	}
}

// T-TRAVERSAL-PUSH-SYMLINK-ESCAPE: a file under a symlinked
// local directory that points outside <project-root> is rejected.
func TestCheckLocalFile_SymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	canon, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CheckLocalFile(root, canon, "escape/secret.txt", filepath.EvalSymlinks)
	if !errors.Is(err, ErrTraversal) {
		t.Fatalf("expected ErrTraversal, got %v", err)
	}
}

// T-CONFINEMENT-SYMLINKED-ROOT-ANCESTOR (local half): an ordinary file
// succeeds when the project root itself sits behind a symlinked ancestor,
// because the check compares against the canonical form, not the
// uncanonicalized root.
func TestCheckLocalFile_SymlinkedProjectRootAncestorSucceeds(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "proj", "main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	root := filepath.Join(link, "proj") // uncanonicalized: goes through the symlinked ancestor
	canon, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := CheckLocalFile(root, canon, "main.go", filepath.EvalSymlinks); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestCheckRemoteReal(t *testing.T) {
	base := "/srv/agents/projects/api"
	if err := CheckRemoteReal(base, base); err != nil {
		t.Fatalf("base itself should be fine: %v", err)
	}
	if err := CheckRemoteReal(base, base+"/sub/dir"); err != nil {
		t.Fatalf("descendant should be fine: %v", err)
	}
	if err := CheckRemoteReal(base, "/srv/agents/other"); !errors.Is(err, ErrTraversal) {
		t.Fatalf("expected ErrTraversal for a real path outside the base, got %v", err)
	}
}
