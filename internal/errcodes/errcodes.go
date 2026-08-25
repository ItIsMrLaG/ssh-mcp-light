// Package errcodes is the one place error codes are defined and errors
// are classified into them, so internal/syncengine (executing a plan) and
// internal/tools (building the final response) can't drift apart on what
// a given failure is called.
package errcodes

const (
	UnknownVM          = "E_UNKNOWN_VM"
	InvalidArgument    = "E_INVALID_ARGUMENT"
	PathTraversal      = "E_PATH_TRAVERSAL"
	FileNotFound       = "E_FILE_NOT_FOUND"
	KeyMissing         = "E_KEY_MISSING"
	KeyUnreadable      = "E_KEY_UNREADABLE"
	KeyEncrypted       = "E_KEY_ENCRYPTED"
	SSHConnectFailed   = "E_SSH_CONNECT_FAILED"
	SSHAuthFailed      = "E_SSH_AUTH_FAILED"
	SFTPFailed         = "E_SFTP_FAILED"
	SSHConnectionLost  = "E_SSH_CONNECTION_LOST"
	TransferTimeout    = "E_TRANSFER_TIMEOUT"
	ConcurrencyTimeout = "E_CONCURRENCY_TIMEOUT"
)

// Classify maps a Go error from the pathsafe/sshlayer layers to one of the
// codes above, by type/sentinel — the single place this mapping happens,
// used by both internal/syncengine and internal/tools.
func Classify(err error) string {
	return classify(err)
}
