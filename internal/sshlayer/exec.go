package sshlayer

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// MaxOutputBytes caps each of stdout/stderr per call, so a runaway
// command can't grow a response without bound.
const MaxOutputBytes = 10 * 1024 * 1024

// quoteArg POSIX single-quotes one element: wrap in ', replacing each
// literal ' inside it with '\”.
func quoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BuildCommand builds the one command string SSH's exec channel actually
// carries. The SSH protocol has no separate argv exec channel — the
// remote server always hands one string to the connecting user's login
// shell — so every element of cmd, args, and cwd is single-quoted here to
// make the remote shell treat each one as an opaque literal. That's what
// makes exec's behavior a direct function of its arguments despite the
// mandatory shell hop: without this quoting, a value containing a space,
// `$`, or `;` would be reinterpreted by the remote shell in ways the
// caller never asked for. Exported so tests can verify the construction
// directly.
func BuildCommand(cmd string, args []string, cwd string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(cmd))
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	quoted := strings.Join(parts, " ")
	if cwd == "" {
		return quoted
	}
	return "cd " + quoteArg(cwd) + " && " + quoted
}

type sshRunner struct {
	client *ssh.Client
}

// Run implements Runner.
func (r *sshRunner) Run(ctx context.Context, cmd string, args []string, cwd string, timeout time.Duration) (Result, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return Result{}, &ConnectionLostError{Err: err}
	}
	var closeOnce sync.Once
	closeSession := func() { closeOnce.Do(func() { _ = session.Close() }) }
	defer closeSession()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return Result{}, &ConnectionLostError{Err: err}
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return Result{}, &ConnectionLostError{Err: err}
	}
	// Stdin is never wired up here (no session.Stdin, no StdinPipe call),
	// so x/crypto/ssh's own Start() sends an immediate channel EOF for it
	// rather than leaving the remote command blocked reading it. No PTY
	// is requested either — RequestPty is simply never called.

	full := BuildCommand(cmd, args, cwd)
	if err := session.Start(full); err != nil {
		return Result{}, &ConnectionLostError{Err: err}
	}

	var res Result
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		res.Stdout, res.TruncatedStdout = readCapped(stdoutPipe, MaxOutputBytes)
		if res.TruncatedStdout {
			closeSession()
		}
	}()
	go func() {
		defer wg.Done()
		res.Stderr, res.TruncatedStderr = readCapped(stderrPipe, MaxOutputBytes)
		if res.TruncatedStderr {
			closeSession()
		}
	}()

	waitDone := make(chan error, 1)
	go func() {
		wg.Wait()
		waitDone <- session.Wait()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case waitErr := <-waitDone:
		// Truncation forced the session closed before the remote process
		// reported its own exit status, so there is no real exit code to
		// report — ExitCode -1 is the reserved "none observed" sentinel
		// (see Result).
		if res.TruncatedStdout || res.TruncatedStderr {
			res.ExitCode = -1
			return res, nil
		}
		switch e := waitErr.(type) {
		case nil:
			res.ExitCode = 0
		case *ssh.ExitError:
			res.ExitCode = e.ExitStatus()
		default:
			res.ExitCode = -1
		}
		return res, nil
	case <-timer.C:
		res.TimedOut = true
		res.ExitCode = -1
		closeSession()
		<-waitDone
		return res, nil
	case <-ctx.Done():
		closeSession()
		<-waitDone
		return res, ctx.Err()
	}
}

// readCapped reads r up to max+1 bytes; if the cap is hit, it returns the
// first max bytes and truncated=true. Reading one byte past max, rather
// than stopping exactly at max, is what lets it distinguish "exactly max
// bytes of output" from "more than max bytes, truncated" in one pass.
func readCapped(r io.Reader, max int) ([]byte, bool) {
	limited := io.LimitReader(r, int64(max)+1)
	data, _ := io.ReadAll(limited)
	if len(data) > max {
		return data[:max], true
	}
	return data, false
}
