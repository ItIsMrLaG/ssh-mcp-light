package errcodes

import (
	"errors"

	"ssh-mcp-light/internal/pathsafe"
	"ssh-mcp-light/internal/sshlayer"
)

func classify(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, pathsafe.ErrTraversal):
		return PathTraversal
	case errors.As(err, new(*sshlayer.KeyMissingError)):
		return KeyMissing
	case errors.As(err, new(*sshlayer.KeyEncryptedError)):
		return KeyEncrypted
	case errors.As(err, new(*sshlayer.KeyUnreadableError)):
		return KeyUnreadable
	case errors.As(err, new(*sshlayer.AuthError)):
		return SSHAuthFailed
	case errors.As(err, new(*sshlayer.ConnectError)):
		return SSHConnectFailed
	case errors.As(err, new(*sshlayer.ConnectionLostError)):
		return SSHConnectionLost
	case errors.As(err, new(*sshlayer.SFTPError)):
		return SFTPFailed
	default:
		return SFTPFailed
	}
}
