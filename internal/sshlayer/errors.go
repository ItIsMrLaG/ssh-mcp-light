package sshlayer

import "fmt"

// These error types let internal/errcodes classify an sshlayer failure by
// Go type (errors.As) rather than by matching error strings, and let
// internal/tools build the right error code without importing sshlayer's
// internals.

// KeyMissingError: identity_file does not exist.
type KeyMissingError struct {
	Path string
	Err  error
}

func (e *KeyMissingError) Error() string {
	return fmt.Sprintf("identity file %q does not exist: %v", e.Path, e.Err)
}
func (e *KeyMissingError) Unwrap() error { return e.Err }

// KeyUnreadableError: identity_file exists but can't be read or parsed.
type KeyUnreadableError struct {
	Path string
	Err  error
}

func (e *KeyUnreadableError) Error() string {
	return fmt.Sprintf("identity file %q is unreadable: %v", e.Path, e.Err)
}
func (e *KeyUnreadableError) Unwrap() error { return e.Err }

// KeyEncryptedError: identity_file is passphrase-protected. There is no
// interactive prompt to ask for one over stdio, so this is always
// terminal, never a retry point.
type KeyEncryptedError struct {
	Path string
}

func (e *KeyEncryptedError) Error() string {
	return fmt.Sprintf("identity file %q is passphrase-protected, which is not supported", e.Path)
}

// ConnectError: TCP dial or SSH handshake failed or timed out.
type ConnectError struct {
	Err error
}

func (e *ConnectError) Error() string { return e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

// AuthError: the SSH server rejected the key.
type AuthError struct {
	Err error
}

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// ConnectionLostError: the SSH session dropped mid-push/sync.
type ConnectionLostError struct {
	Err error
}

func (e *ConnectionLostError) Error() string { return e.Err.Error() }
func (e *ConnectionLostError) Unwrap() error { return e.Err }

// SFTPError: an SFTP operation failed for a reason none of the above
// covers.
type SFTPError struct {
	Err error
}

func (e *SFTPError) Error() string { return e.Err.Error() }
func (e *SFTPError) Unwrap() error { return e.Err }
