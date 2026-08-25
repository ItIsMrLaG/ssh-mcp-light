package tools_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExec_Basic(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("exec", map[string]any{"vm": "testvm", "cmd": "echo", "args": []string{"hi"}})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	if out["stdout"] != "hi\n" {
		t.Fatalf("stdout = %v, want %q", out["stdout"], "hi\n")
	}
	if out["exit_code"].(float64) != 0 {
		t.Fatalf("exit_code = %v, want 0", out["exit_code"])
	}
}

// exec still isn't path-confined once wired through the whole tool stack,
// not just the SSH layer underneath it.
func TestExec_OutsideRemoteRootSucceedsThroughTool(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("exec", map[string]any{"vm": "testvm", "cmd": "cat", "args": []string{"/etc/hostname"}})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	if out["exit_code"].(float64) != 0 {
		t.Fatalf("exit_code = %v, want 0", out["exit_code"])
	}
}

func TestPush_Success(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.writeLocal("cmd/api/main.go", "package main")
	e.writeLocal("go.mod", "module x")

	res, out := e.call("push", map[string]any{
		"vm":    "testvm",
		"files": []string{"cmd/api/main.go", "go.mod"},
		"dest":  "./",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	uploaded, _ := out["uploaded"].([]any)
	if len(uploaded) != 2 {
		t.Fatalf("uploaded = %v, want 2 entries", out["uploaded"])
	}
	content, err := os.ReadFile(filepath.Join(e.server.Root, "projects/api", "cmd/api/main.go"))
	if err != nil || string(content) != "package main" {
		t.Fatalf("remote file missing or wrong content: %v %q", err, content)
	}
}

// T-PUSH-IGNORE-SKIP.
func TestPush_IgnoreSkip(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.writeLocal("internal/testdata/fixture.log", "noise")
	e.writeLocal("main.go", "package main")
	e.deps().ProjectConfig.Ignore = []string{"*.log"}

	res, out := e.call("push", map[string]any{
		"vm":    "testvm",
		"files": []string{"internal/testdata/fixture.log", "main.go"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	skipped, _ := out["skipped_by_ignore"].([]any)
	if len(skipped) != 1 || skipped[0] != "internal/testdata/fixture.log" {
		t.Fatalf("skipped_by_ignore = %v", out["skipped_by_ignore"])
	}
	uploaded, _ := out["uploaded"].([]any)
	if len(uploaded) != 1 {
		t.Fatalf("uploaded = %v, want 1", out["uploaded"])
	}
}

// T-PUSH-FILE-NOT-FOUND.
func TestPush_FileNotFound(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("push", map[string]any{
		"vm":    "testvm",
		"files": []string{"does-not-exist.txt"},
	})
	if res.IsError {
		t.Fatalf("push itself is not a top-level error for a per-file miss: %v", out)
	}
	failed, _ := out["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("failed = %v, want 1 entry", out["failed"])
	}
	entry := failed[0].(map[string]any)
	if entry["error_code"] != "E_FILE_NOT_FOUND" {
		t.Fatalf("error_code = %v, want E_FILE_NOT_FOUND", entry["error_code"])
	}
}

// T-TRAVERSAL-PUSH-FILES-DOTDOT / ABSOLUTE.
func TestPush_TraversalRejected(t *testing.T) {
	e := newEnv(t, "projects/api")
	res, out := e.call("push", map[string]any{
		"vm":    "testvm",
		"files": []string{"../escape.txt"},
	})
	if res.IsError {
		t.Fatalf("unexpected top-level error: %v", out)
	}
	failed, _ := out["failed"].([]any)
	if len(failed) != 1 || failed[0].(map[string]any)["error_code"] != "E_PATH_TRAVERSAL" {
		t.Fatalf("failed = %v, want one E_PATH_TRAVERSAL entry", out["failed"])
	}
}

// T-TRAVERSAL-PUSH-DEST-DOTDOT.
func TestPush_DestTraversalRejected(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.writeLocal("main.go", "package main")
	res, out := e.call("push", map[string]any{
		"vm":    "testvm",
		"files": []string{"main.go"},
		"dest":  "../outside",
	})
	if res.IsError {
		t.Fatalf("unexpected top-level error: %v", out)
	}
	failed, _ := out["failed"].([]any)
	if len(failed) != 1 || failed[0].(map[string]any)["error_code"] != "E_PATH_TRAVERSAL" {
		t.Fatalf("failed = %v, want one E_PATH_TRAVERSAL entry", out["failed"])
	}
}

func TestSync_DryRunAndRealRun(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.writeLocal("cmd/api/main.go", "package main")
	e.writeRemote("old_binary", "stale")

	// Dry run: plans, no writes.
	res, out := e.call("sync", map[string]any{"vm": "testvm", "dry_run": true})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	toUpload, _ := out["to_upload"].([]any)
	toDelete, _ := out["to_delete"].([]any)
	if len(toUpload) != 1 || toUpload[0] != "cmd/api/main.go" {
		t.Fatalf("to_upload = %v", out["to_upload"])
	}
	if len(toDelete) != 1 || toDelete[0] != "old_binary" {
		t.Fatalf("to_delete = %v, want [old_binary]: must be remote-base-relative, no remote_local_root prefix", out["to_delete"])
	}
	if _, err := os.Stat(filepath.Join(e.server.Root, "projects/api", "cmd/api/main.go")); err == nil {
		t.Fatalf("dry_run must not have uploaded anything")
	}
	if _, err := os.Stat(filepath.Join(e.server.Root, "projects/api", "old_binary")); err != nil {
		t.Fatalf("dry_run must not have deleted anything: %v", err)
	}

	// Real run.
	res2, out2 := e.call("sync", map[string]any{"vm": "testvm"})
	if res2.IsError {
		t.Fatalf("unexpected error: %v", out2)
	}
	uploaded, _ := out2["uploaded"].([]any)
	deleted, _ := out2["deleted"].([]any)
	if len(uploaded) != 1 || uploaded[0] != "cmd/api/main.go" {
		t.Fatalf("uploaded = %v", out2["uploaded"])
	}
	if len(deleted) != 1 || deleted[0] != "old_binary" {
		t.Fatalf("deleted = %v", out2["deleted"])
	}
	if _, err := os.Stat(filepath.Join(e.server.Root, "projects/api", "cmd/api/main.go")); err != nil {
		t.Fatalf("expected file to have been uploaded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.server.Root, "projects/api", "old_binary")); !os.IsNotExist(err) {
		t.Fatalf("expected old_binary to have been deleted")
	}
}

// T-SYNC-IGNORE-PROTECTS-FROM-DELETE through the full tool stack.
func TestSync_IgnoreProtectsFromDelete(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.deps().ProjectConfig.Ignore = []string{"*.log"}
	e.writeRemote("app.log", "keep me")

	res, out := e.call("sync", map[string]any{"vm": "testvm"})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	protected, _ := out["protected_by_ignore"].([]any)
	if len(protected) != 1 || protected[0] != "app.log" {
		t.Fatalf("protected_by_ignore = %v", out["protected_by_ignore"])
	}
	if _, err := os.Stat(filepath.Join(e.server.Root, "projects/api", "app.log")); err != nil {
		t.Fatalf("app.log must not have been deleted: %v", err)
	}
}

// T-SYNC-UPLOAD-ATOMIC-NO-PARTIAL-FILE: after a successful
// sync, no stray .*.tmp temporary file is left behind.
func TestSync_AtomicUploadLeavesNoTempFile(t *testing.T) {
	e := newEnv(t, "projects/api")
	e.writeLocal("main.go", "package main")

	res, out := e.call("sync", map[string]any{"vm": "testvm"})
	if res.IsError {
		t.Fatalf("unexpected error: %v", out)
	}
	entries, err := os.ReadDir(filepath.Join(e.server.Root, "projects/api"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if filepath.Ext(ent.Name()) == ".tmp" {
			t.Fatalf("found leftover temp file %q", ent.Name())
		}
	}
}
