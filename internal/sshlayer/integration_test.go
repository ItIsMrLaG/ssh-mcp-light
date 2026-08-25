package sshlayer_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"ssh-mcp-light/internal/config"
	"ssh-mcp-light/internal/sshlayer"
	"ssh-mcp-light/internal/sshtest"

	"golang.org/x/crypto/ssh"
)

func testVM(t *testing.T, s *sshtest.Server) config.VM {
	t.Helper()
	host, port := s.SplitHostPort()
	return config.VM{
		Name:         "testvm",
		Address:      host,
		Port:         port,
		User:         s.AuthorizedUser,
		IdentityFile: sshtest.WriteKeyFile(t, s),
		RemoteRoot:   s.Root,
	}
}

// Smoke test for the sshtest fixture itself plus sshlayer.Connector end to
// end: dial, exec a real command, read its real output and exit code.
func TestConnector_ExecSmoke(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)

	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	res, err := runner.Run(context.Background(), "echo", []string{"hello"}, "", 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Stdout) != "hello\n" {
		t.Fatalf("stdout = %q, want %q", res.Stdout, "hello\n")
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
}

// T-EXEC-NONZERO-EXIT-NOT-ERROR: a non-zero exit is a normal
// response, not a Go error from Run.
func TestConnector_Exec_NonZeroExitIsNotAnError(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	res, err := runner.Run(context.Background(), "sh", []string{"-c", "exit 3"}, "", 5*time.Second)
	if err != nil {
		t.Fatalf("Run returned an error for a non-zero exit: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
}

// T-EXEC-TIMEOUT.
func TestConnector_Exec_Timeout(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	res, err := runner.Run(context.Background(), "sleep", []string{"5"}, "", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("expected TimedOut=true")
	}
	if res.ExitCode != -1 {
		t.Fatalf("exit code = %d, want -1", res.ExitCode)
	}
}

// T-EXEC-STDOUT-TRUNCATION.
func TestConnector_Exec_StdoutTruncation(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	// Print well over the cap using a small, fast-terminating loop instead
	// of dd (which may not be present on every CI image).
	cmd := "yes x | head -c 12000000"
	res, err := runner.Run(context.Background(), "sh", []string{"-c", cmd}, "", 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.TruncatedStdout {
		t.Fatalf("expected TruncatedStdout=true")
	}
	if len(res.Stdout) != sshlayer.MaxOutputBytes {
		t.Fatalf("stdout length = %d, want %d", len(res.Stdout), sshlayer.MaxOutputBytes)
	}
	if res.ExitCode != -1 {
		t.Fatalf("exit code = %d, want -1", res.ExitCode)
	}
}

// T-EXEC-NO-TTY-NO-STDIN.
func TestConnector_Exec_NoTTYNoStdin(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	// `cat` reading stdin must see immediate EOF, not block.
	res, err := runner.Run(context.Background(), "cat", nil, "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TimedOut {
		t.Fatalf("cat blocked on stdin instead of seeing immediate EOF")
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}

	// `test -t 0` must report "not a tty".
	res2, err := runner.Run(context.Background(), "test", []string{"-t", "0"}, "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.ExitCode == 0 {
		t.Fatalf("expected no TTY on stdin (test -t 0 to fail), got exit 0")
	}
}

// T-EXEC-OUTSIDE-REMOTE-ROOT-SUCCEEDS: exec reaching a path
// outside <remote-root> succeeds.
func TestConnector_Exec_OutsideRemoteRootSucceeds(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	res, err := runner.Run(context.Background(), "cat", []string{"/etc/hostname"}, "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (exec is not path-confined)", res.ExitCode)
	}
}

// T-EXEC-QUOTING-SPECIAL-CHARS, integration half: the
// remote process receives each argv element verbatim, and a cwd
// containing a space is honored via the cd prefix.
func TestConnector_Exec_QuotingSpecialChars(t *testing.T) {
	s := sshtest.Start(t)
	vm := testVM(t, s)
	c := sshlayer.NewConnector()
	runner, _, closeFn, err := c.Connect(context.Background(), vm)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = closeFn() }()

	arg := `it's $HOME; rm -rf /; ` + "`whoami`"
	// printf %s emits the argv element with no shell reinterpretation on
	// the remote end; if quoting were broken, the command would either
	// fail to parse or emit something other than the literal argument.
	res, err := runner.Run(context.Background(), "printf", []string{"%s", arg}, "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(res.Stdout) != arg {
		t.Fatalf("stdout = %q, want %q (argv element was reinterpreted)", res.Stdout, arg)
	}

	dirWithSpace := s.Root + "/dir with space"
	if err := os.MkdirAll(dirWithSpace, 0o755); err != nil {
		t.Fatal(err)
	}
	res2, err := runner.Run(context.Background(), "pwd", nil, dirWithSpace, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(res2.Stdout) != dirWithSpace+"\n" {
		t.Fatalf("pwd = %q, want %q", res2.Stdout, dirWithSpace+"\n")
	}
}

// T-CONNECT-TIMEOUT: a peer that accepts the TCP
// connection but never speaks SSH makes the handshake hang; Connect must
// still fail within the 10 second combined dial+handshake budget, on
// localhost, deterministically (no dependency on how the sandbox's
// network handles an unroutable address).
func TestConnector_ConnectTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept but never write the SSH banner: the handshake hangs.
			go func() { _, _ = io.Copy(io.Discard, conn) }()
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	dir := t.TempDir()
	keyPath := dir + "/id_test"
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = signer

	vm := config.VM{
		Name: "hung", Address: host, Port: port,
		User: "nobody", IdentityFile: keyPath, RemoteRoot: "/",
	}
	c := sshlayer.NewConnector()
	start := time.Now()
	_, _, _, err = c.Connect(context.Background(), vm)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected a connect error")
	}
	if elapsed > sshlayer.ConnectTimeout+2*time.Second {
		t.Fatalf("connect took %v, expected to be bounded by the %v budget", elapsed, sshlayer.ConnectTimeout)
	}
	if elapsed < sshlayer.ConnectTimeout-time.Second {
		t.Fatalf("connect returned after only %v, before the %v budget elapsed", elapsed, sshlayer.ConnectTimeout)
	}
}
