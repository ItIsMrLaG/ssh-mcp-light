package syncengine

import (
	"fmt"
	"os"
	"path"

	"testing"

	"ssh-mcp-light/internal/sshlayer"
)

// fakeTransfer is a minimal, in-memory sshlayer.FileTransfer double for
// exercising Apply's partial-failure and connection-lost handling without
// a live connection.
type fakeTransfer struct {
	failUpload map[string]error // remotePath -> error to return from Upload
	dead       bool             // once true, every operation fails with ConnectionLostError

	uploaded []string
	removed  []string
}

func (f *fakeTransfer) errIfDead() error {
	if f.dead {
		return &sshlayer.ConnectionLostError{Err: fmt.Errorf("connection lost")}
	}
	return nil
}

func (f *fakeTransfer) Stat(string) (sshlayer.FileInfo, bool, error) {
	return sshlayer.FileInfo{}, false, nil
}
func (f *fakeTransfer) ReadDirRecursive(string) ([]sshlayer.FileInfo, error) { return nil, nil }
func (f *fakeTransfer) Upload(localPath, remotePath string, _ os.FileMode) error {
	if err := f.errIfDead(); err != nil {
		return err
	}
	if err, ok := f.failUpload[remotePath]; ok {
		return err
	}
	f.uploaded = append(f.uploaded, remotePath)
	return nil
}
func (f *fakeTransfer) Remove(remotePath string) error {
	if err := f.errIfDead(); err != nil {
		return err
	}
	f.removed = append(f.removed, remotePath)
	return nil
}
func (f *fakeTransfer) RemoveDir(remotePath string) error  { return f.Remove(remotePath) }
func (f *fakeTransfer) MkdirAll(string, os.FileMode) error { return f.errIfDead() }
func (f *fakeTransfer) RealPath(remotePath string) (string, error) {
	if err := f.errIfDead(); err != nil {
		return "", err
	}
	return remotePath, nil
}
func (f *fakeTransfer) Rename(string, string) error { return f.errIfDead() }

var _ sshlayer.FileTransfer = (*fakeTransfer)(nil)

// T-SYNC-PARTIAL-FAILURE-CONTINUES.
func TestApply_PartialFailureContinues(t *testing.T) {
	base := "/srv/agents/projects/api"
	failPath := path.Join(base, "bad.txt")
	f := &fakeTransfer{failUpload: map[string]error{failPath: &sshlayer.SFTPError{Err: fmt.Errorf("disk full")}}}

	plan := &Plan{
		ToUpload: []PlannedUpload{
			{RelPath: "bad.txt", LocalPath: "/local/bad.txt"},
			{RelPath: "good.txt", LocalPath: "/local/good.txt"},
		},
	}
	result := Apply(f, base, base, plan)

	if len(result.Uploaded) != 1 || result.Uploaded[0] != "good.txt" {
		t.Fatalf("Uploaded = %v, want [good.txt]", result.Uploaded)
	}
	if len(result.Failed) != 1 || result.Failed[0].Path != "bad.txt" {
		t.Fatalf("Failed = %v, want one entry for bad.txt", result.Failed)
	}
	if result.Failed[0].ErrorCode != "E_SFTP_FAILED" {
		t.Fatalf("ErrorCode = %v, want E_SFTP_FAILED", result.Failed[0].ErrorCode)
	}
}

// T-SYNC-CONNECTION-LOST-ABORTS-REMAINING: once the connection
// dies, every not-yet-attempted action ends up in Failed with
// E_SSH_CONNECTION_LOST.
func TestApply_ConnectionLostAbortsRemaining(t *testing.T) {
	base := "/srv/agents/projects/api"
	f := &fakeTransfer{dead: true}

	plan := &Plan{
		ToUpload: []PlannedUpload{
			{RelPath: "a.txt", LocalPath: "/local/a.txt"},
			{RelPath: "b.txt", LocalPath: "/local/b.txt"},
		},
		ToDelete: []PlannedDelete{
			{RelPath: "old.txt"},
		},
	}
	result := Apply(f, base, base, plan)

	if len(result.Uploaded) != 0 || len(result.Deleted) != 0 {
		t.Fatalf("expected nothing to succeed once the connection is lost, got uploaded=%v deleted=%v", result.Uploaded, result.Deleted)
	}
	if len(result.Failed) != 3 {
		t.Fatalf("expected all 3 planned actions to be reported failed, got %+v", result.Failed)
	}
	for _, fa := range result.Failed {
		if fa.ErrorCode != "E_SSH_CONNECTION_LOST" {
			t.Fatalf("action %q: error_code = %v, want E_SSH_CONNECTION_LOST", fa.Path, fa.ErrorCode)
		}
	}
}

// T-TRAVERSAL-SYNC-DELETE: a delete candidate whose
// real remote path (as reported by RealPath) resolves outside the
// canonical remote base is rejected, not deleted.
func TestApply_DeleteRevalidatesRealPath(t *testing.T) {
	base := "/srv/agents/projects/api"
	f := &realPathOverrideTransfer{
		fakeTransfer: fakeTransfer{},
		override:     map[string]string{path.Join(base, "escape"): "/srv/agents/other/escape"},
	}
	plan := &Plan{ToDelete: []PlannedDelete{{RelPath: "escape"}}}
	result := Apply(f, base, base, plan)

	if len(result.Deleted) != 0 {
		t.Fatalf("expected the delete to be rejected, got Deleted=%v", result.Deleted)
	}
	if len(result.Failed) != 1 || result.Failed[0].ErrorCode != "E_PATH_TRAVERSAL" {
		t.Fatalf("Failed = %+v, want one E_PATH_TRAVERSAL entry", result.Failed)
	}
}

type realPathOverrideTransfer struct {
	fakeTransfer
	override map[string]string
}

func (f *realPathOverrideTransfer) RealPath(remotePath string) (string, error) {
	if r, ok := f.override[remotePath]; ok {
		return r, nil
	}
	return remotePath, nil
}
