package sshlayer

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"ssh-mcp-light/internal/config"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ConnectTimeout bounds the TCP dial and the SSH handshake together, not
// each separately — a slow dial should leave less time for the handshake,
// not reset the clock, so a peer can't stall a connection attempt for
// twice this long just by being slow in two different ways.
const ConnectTimeout = 10 * time.Second

// MaxConcurrentPerVM bounds concurrent in-flight connections to one VM.
// Connector opens a fresh SSH+SFTP connection per call rather than
// pooling/sharing one across calls — simpler, and it keeps one call's
// success independent of another's connection state — so this semaphore
// caps concurrent connections directly rather than channels multiplexed
// over a shared connection.
const MaxConcurrentPerVM = 4

// Connector is the real VMConnector implementation.
type Connector struct {
	Policy HostKeyPolicy // nil means AcceptAllHostKeys

	mu   sync.Mutex
	sems map[string]chan struct{}
}

// NewConnector returns a Connector that accepts any host key (see
// AcceptAllHostKeys).
func NewConnector() *Connector {
	return &Connector{Policy: AcceptAllHostKeys{}}
}

func (c *Connector) semaphore(vmName string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sems == nil {
		c.sems = make(map[string]chan struct{})
	}
	s, ok := c.sems[vmName]
	if !ok {
		s = make(chan struct{}, MaxConcurrentPerVM)
		c.sems[vmName] = s
	}
	return s
}

// loadSigner reads identity_file lazily, on first connection to that VM,
// rather than at startup — a server configured with VMs it never
// contacts in a given session shouldn't fail to start over one of them.
func loadSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &KeyMissingError{Path: path, Err: err}
		}
		return nil, &KeyUnreadableError{Path: path, Err: err}
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return nil, &KeyEncryptedError{Path: path}
		}
		return nil, &KeyUnreadableError{Path: path, Err: err}
	}
	return signer, nil
}

// dial performs the TCP dial and SSH handshake within one ConnectTimeout
// budget. ssh.Dial's own Timeout field only bounds the dial, not the
// handshake that follows — so the deadline here is set on the net.Conn
// directly, which bounds both steps together, then cleared once the
// handshake succeeds (later per-call timeouts are the caller's job).
func dial(ctx context.Context, addr string, sshConfig *ssh.ClientConfig) (*ssh.Client, error) {
	deadline := time.Now().Add(ConnectTimeout)
	d := net.Dialer{Timeout: ConnectTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &ConnectError{Err: err}
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, &ConnectError{Err: err}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		_ = conn.Close()
		if isAuthError(err) {
			return nil, &AuthError{Err: err}
		}
		return nil, &ConnectError{Err: err}
	}
	_ = conn.SetDeadline(time.Time{}) // per-call timeouts are handled by exec/sftp callers from here
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func isAuthError(err error) bool {
	// x/crypto/ssh reports failed auth as a plain error whose message
	// contains "unable to authenticate"; there is no exported sentinel.
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain")
}

// Connect implements VMConnector: it loads the VM's key, dials and
// authenticates, and returns a Runner and FileTransfer bound to one SSH
// connection plus SFTP session, released by the returned close func.
func (c *Connector) Connect(ctx context.Context, vm config.VM) (Runner, FileTransfer, func() error, error) {
	sem := c.semaphore(vm.Name)
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, nil, ctx.Err()
	}
	release := func() { <-sem }

	signer, err := loadSigner(vm.IdentityFile)
	if err != nil {
		release()
		return nil, nil, nil, err
	}

	policy := c.Policy
	if policy == nil {
		policy = AcceptAllHostKeys{}
	}
	sshConfig := &ssh.ClientConfig{
		User: vm.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return policy.Accept(vm.Name, vm.Address, key)
		},
		Timeout: ConnectTimeout,
	}

	addr := fmt.Sprintf("%s:%d", vm.Address, vm.Port)
	client, err := dial(ctx, addr, sshConfig)
	if err != nil {
		release()
		return nil, nil, nil, err
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		release()
		return nil, nil, nil, &SFTPError{Err: err}
	}

	closed := false
	closeFn := func() error {
		if closed {
			return nil
		}
		closed = true
		_ = sftpClient.Close()
		err := client.Close()
		release()
		return err
	}

	runner := &sshRunner{client: client}
	transfer := &sftpTransfer{client: sftpClient}
	return runner, transfer, closeFn, nil
}
