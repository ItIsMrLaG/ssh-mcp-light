package syncengine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ssh-mcp-light/internal/sshlayer"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// T-SYNC-UPLOAD-NEW, T-SYNC-UPLOAD-SIZE-CHANGED,
// T-SYNC-SKIP-SAME-SIZE-OLDER-MTIME.
func TestBuildPlan_ComparisonBasis(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "new.txt", "hello")                // remote-absent: upload
	writeFile(t, root, "changed.txt", "new-content-here") // size differs: upload
	writeFile(t, root, "unchanged.txt", "same")           // same size, older mtime: skip

	now := time.Now()
	remote := []sshlayer.FileInfo{
		{Path: "changed.txt", Size: 3, ModTime: now},                  // local is 17 bytes, remote 3: differs
		{Path: "unchanged.txt", Size: 4, ModTime: now.Add(time.Hour)}, // same size (local "same"=4), remote newer: skip
	}

	plan, err := BuildPlan(root, remote, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, u := range plan.ToUpload {
		got[u.RelPath] = true
	}
	if !got["new.txt"] {
		t.Errorf("expected new.txt to be planned for upload")
	}
	if !got["changed.txt"] {
		t.Errorf("expected changed.txt to be planned for upload")
	}
	if got["unchanged.txt"] {
		t.Errorf("expected unchanged.txt to be skipped (same size, not newer)")
	}
}

// T-SYNC-DELETE-REMOTE-ONLY.
func TestBuildPlan_DeleteRemoteOnly(t *testing.T) {
	root := t.TempDir()
	remote := []sshlayer.FileInfo{
		{Path: "old_binary", Size: 10, ModTime: time.Now()},
	}
	plan, err := BuildPlan(root, remote, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ToDelete) != 1 || plan.ToDelete[0].RelPath != "old_binary" {
		t.Fatalf("expected old_binary to be planned for deletion, got %+v", plan.ToDelete)
	}
}

// T-SYNC-SKIP-SYMLINK: a local symlink is excluded from upload
// and listed in SkippedSymlinks.
func TestBuildPlan_SkipLocalSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "real.txt", "x")
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	plan, err := BuildPlan(root, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range plan.ToUpload {
		if u.RelPath == "link.txt" {
			t.Fatalf("symlink must not be planned for upload")
		}
	}
	found := false
	for _, s := range plan.SkippedSymlinks {
		if s == "link.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected link.txt in SkippedSymlinks, got %v", plan.SkippedSymlinks)
	}
}

// T-SYNC-EMPTY-DIR-NOT-CREATED / T-SYNC-EMPTY-DIR-DELETED-REMOTE-ONLY.
func TestBuildPlan_EmptyDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	remote := []sshlayer.FileInfo{
		{Path: "remote_empty_dir", IsDir: true, ModTime: time.Now()},
	}
	plan, err := BuildPlan(root, remote, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range plan.ToUpload {
		if u.RelPath == "empty" {
			t.Fatalf("an empty local directory must not itself be planned for upload")
		}
	}
	if len(plan.ToDelete) != 1 || plan.ToDelete[0].RelPath != "remote_empty_dir" || !plan.ToDelete[0].IsDir {
		t.Fatalf("expected the empty remote-only directory to be planned for deletion, got %+v", plan.ToDelete)
	}
}

// T-SYNC-IGNORE-PROTECTS-FROM-DELETE: a remote-only file
// whose path matches an ignore pattern is protected, not deleted.
func TestBuildPlan_IgnoreProtectsFromDelete(t *testing.T) {
	root := t.TempDir()
	remote := []sshlayer.FileInfo{
		{Path: "app.log", Size: 1, ModTime: time.Now()},
		{Path: "old_binary", Size: 1, ModTime: time.Now()},
	}
	plan, err := BuildPlan(root, remote, nil, []string{"*.log"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plan.ToDelete {
		if d.RelPath == "app.log" {
			t.Fatalf("app.log matches an ignore pattern and must not be deleted")
		}
	}
	protected := false
	for _, p := range plan.ProtectedByIgnore {
		if p == "app.log" {
			protected = true
		}
	}
	if !protected {
		t.Fatalf("expected app.log in ProtectedByIgnore, got %v", plan.ProtectedByIgnore)
	}
	deletedOldBinary := false
	for _, d := range plan.ToDelete {
		if d.RelPath == "old_binary" {
			deletedOldBinary = true
		}
	}
	if !deletedOldBinary {
		t.Fatalf("expected old_binary (not matching any ignore pattern) to still be deleted")
	}
}

// A directory containing only protected entries must itself be protected,
// not deleted.
func TestBuildPlan_DirectoryWithOnlyProtectedChildrenIsNotDeleted(t *testing.T) {
	root := t.TempDir()
	remote := []sshlayer.FileInfo{
		{Path: "logs", IsDir: true, ModTime: time.Now()},
		{Path: "logs/app.log", Size: 1, ModTime: time.Now()},
	}
	plan, err := BuildPlan(root, remote, nil, []string{"logs/"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plan.ToDelete {
		if d.RelPath == "logs" || d.RelPath == "logs/app.log" {
			t.Fatalf("expected %q to be protected, not deleted; ToDelete=%v", d.RelPath, plan.ToDelete)
		}
	}
}

// T-SYNC-RESPONSE-PATH-BASES: to_upload is relative to
// <project-root>; to_delete is relative to the remote base, with no
// remote_local_root double-prefixing (the remote base is passed to
// ReadDirRecursive by the caller, so entries here are already
// remote-base-relative; BuildPlan must not add any further prefix).
func TestBuildPlan_ResponsePathBases(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "cmd/api/main.go", "package main")
	remote := []sshlayer.FileInfo{
		{Path: "old_binary", Size: 1, ModTime: time.Now()},
	}
	plan, err := BuildPlan(root, remote, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ToUpload) != 1 || plan.ToUpload[0].RelPath != "cmd/api/main.go" {
		t.Fatalf("to_upload = %+v, want [cmd/api/main.go]", plan.ToUpload)
	}
	if len(plan.ToDelete) != 1 || plan.ToDelete[0].RelPath != "old_binary" {
		t.Fatalf("to_delete = %+v, want [old_binary] (no remote_local_root prefix)", plan.ToDelete)
	}
}
