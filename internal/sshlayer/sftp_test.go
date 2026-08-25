package sshlayer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ssh-mcp-light/internal/sshlayer"
	"ssh-mcp-light/internal/sshtest"
)

// T-SYNC-UPLOAD-ATOMIC-NO-PARTIAL-FILE, FileTransfer.Upload
// half: when the write fails partway through, the destination path is
// left absent (new file) rather than partially written, and the temporary
// file is cleaned up.
func TestSFTPTransfer_UploadFailureLeavesNoPartialFile(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	_, transfer, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	// A destination directory the SFTP user cannot write into makes the
	// temporary file's create/write/rename fail partway through.
	roDir := filepath.Join(s.Root, "readonly")
	if err := os.MkdirAll(roDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) }) // let TempDir cleanup remove it

	local := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(local, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(roDir, "dest.txt")
	uploadErr := transfer.Upload(local, dest, 0o644)
	if uploadErr == nil {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits are not enforced")
		}
		t.Fatalf("expected the upload into a read-only directory to fail")
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination must not exist after a failed upload, stat err = %v", err)
	}
	entries, err := os.ReadDir(roDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("leftover temp file %q after a failed upload", e.Name())
		}
	}
}

// The atomic-upload happy path: the destination ends up with the full
// content in one step, never a truncated intermediate state visible under
// its final name.
func TestSFTPTransfer_UploadSuccessIsAtomic(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	_, transfer, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	local := filepath.Join(t.TempDir(), "src.txt")
	content := "the quick brown fox"
	if err := os.WriteFile(local, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(s.Root, "dest.txt")

	if err := transfer.Upload(local, dest, 0o644); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != content {
		t.Fatalf("dest content = %q, err = %v, want %q", got, err, content)
	}

	entries, err := os.ReadDir(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("leftover temp file %q after a successful upload", e.Name())
		}
	}
}
