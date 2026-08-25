package tools

import (
	"fmt"

	"ssh-mcp-light/internal/config"
	"ssh-mcp-light/internal/errcodes"
)

// Local aliases for internal/errcodes' constants, so call sites in this
// package stay terse.
const (
	codeUnknownVM          = errcodes.UnknownVM
	codeInvalidArgument    = errcodes.InvalidArgument
	codePathTraversal      = errcodes.PathTraversal
	codeFileNotFound       = errcodes.FileNotFound
	codeKeyMissing         = errcodes.KeyMissing
	codeKeyUnreadable      = errcodes.KeyUnreadable
	codeKeyEncrypted       = errcodes.KeyEncrypted
	codeSSHConnectFailed   = errcodes.SSHConnectFailed
	codeSSHAuthFailed      = errcodes.SSHAuthFailed
	codeSFTPFailed         = errcodes.SFTPFailed
	codeSSHConnectionLost  = errcodes.SSHConnectionLost
	codeTransferTimeout    = errcodes.TransferTimeout
	codeConcurrencyTimeout = errcodes.ConcurrencyTimeout
)

// classifyErr maps an sshlayer/pathsafe error to its error code.
func classifyErr(err error) string {
	return errcodes.Classify(err)
}

func unknownVM(name string) (msg string) {
	return fmt.Sprintf("unknown VM %q: not present in VM config", name)
}

// lookupVM is a thin pass-through kept here (rather than calling
// cfg.Lookup directly at call sites) so every unknown-VM code path in this
// package reads the same way.
func lookupVM(cfg *config.VMConfig, name string) (config.VM, bool) {
	return cfg.Lookup(name)
}

// connectMessage renders an sshlayer connection error into a message
// naming the specific field (identity file, address:port, user) that
// caused it, rather than just forwarding the underlying Go error text.
func connectMessage(code string, vm config.VM, err error) string {
	switch code {
	case codeKeyMissing:
		return fmt.Sprintf("identity file %q for VM %q does not exist", vm.IdentityFile, vm.Name)
	case codeKeyUnreadable:
		return fmt.Sprintf("identity file %q for VM %q is unreadable: %v", vm.IdentityFile, vm.Name, err)
	case codeKeyEncrypted:
		return fmt.Sprintf("identity file %q for VM %q is passphrase-protected, which is not supported", vm.IdentityFile, vm.Name)
	case codeSSHConnectFailed:
		return fmt.Sprintf("could not connect to VM %q at %s:%d: %v", vm.Name, vm.Address, vm.Port, err)
	case codeSSHAuthFailed:
		return fmt.Sprintf("SSH authentication failed for VM %q as user %q: %v", vm.Name, vm.User, err)
	default:
		return err.Error()
	}
}
