// Package sshlayer is the only package that imports golang.org/x/crypto/
// ssh and github.com/pkg/sftp. Everything else depends on it only through
// the interfaces below, never through concrete types, so tests can
// substitute an in-process fake instead of dialing a real VM.
package sshlayer

import (
	"context"
	"os"
	"time"

	"ssh-mcp-light/internal/config"

	"golang.org/x/crypto/ssh"
)

// Result is the outcome of one exec call. ExitCode is -1, a reserved
// sentinel, whenever TimedOut or either Truncated* field is true — in all
// of those cases the session was closed before the remote process
// reported a real exit status, so there is nothing genuine to report.
type Result struct {
	Stdout, Stderr  []byte
	ExitCode        int
	TimedOut        bool
	TruncatedStdout bool
	TruncatedStderr bool
}

// Runner executes one command on a VM.
type Runner interface {
	Run(ctx context.Context, cmd string, args []string, cwd string, timeout time.Duration) (Result, error)
}

// FileInfo describes one remote filesystem entry.
type FileInfo struct {
	Path    string // relative to the directory that was listed
	Size    int64
	ModTime time.Time
	IsDir   bool
	IsLink  bool
}

// FileTransfer is the SFTP-facing surface push/sync depend on.
type FileTransfer interface {
	Stat(remotePath string) (FileInfo, bool, error) // ok=false if not found
	ReadDirRecursive(remotePath string) ([]FileInfo, error)
	Upload(localPath, remotePath string, mode os.FileMode) error
	Remove(remotePath string) error
	RemoveDir(remotePath string) error
	MkdirAll(remotePath string, mode os.FileMode) error
	RealPath(remotePath string) (string, error)
	Rename(oldPath, newPath string) error
}

// HostKeyPolicy is the one seam host key trust decisions go through. A
// future trust-on-first-use or pinning policy plugs in here, behind a
// per-VM config field, without touching the tool surface or error
// taxonomy at all — that's the reason this is an interface with exactly
// one method rather than a bool flag threaded through every caller.
type HostKeyPolicy interface {
	Accept(vmName, address string, key ssh.PublicKey) error
}

// AcceptAllHostKeys is this version's HostKeyPolicy: it accepts whatever
// key the server presents, unconditionally. Host key verification isn't
// implemented yet — this is the seam it will attach to.
type AcceptAllHostKeys struct{}

// Accept implements HostKeyPolicy.
func (AcceptAllHostKeys) Accept(string, string, ssh.PublicKey) error { return nil }

// VMConnector produces a Runner and a FileTransfer for a named VM. The
// returned close func releases the connection; every caller must invoke
// it, since Connector opens a fresh connection per call rather than
// pooling one (see Connector's doc comment for why).
type VMConnector interface {
	Connect(ctx context.Context, vm config.VM) (Runner, FileTransfer, func() error, error)
}
