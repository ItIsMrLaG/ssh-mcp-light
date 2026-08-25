package tools

import (
	"context"

	"ssh-mcp-light/internal/config"
	"ssh-mcp-light/internal/sshlayer"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const remoteDirMode = 0o755

// connectOrFail dials vm and turns a connection failure into a failure()
// result with the right error code, so every tool handler that needs a
// connection gets identical unknown-VM/key/auth error handling for free
// instead of repeating the classify-and-wrap logic five times.
func connectOrFail(ctx context.Context, connector sshlayer.VMConnector, vm config.VM) (sshlayer.Runner, sshlayer.FileTransfer, func() error, *mcp.CallToolResult) {
	runner, transfer, closeFn, err := connector.Connect(ctx, vm)
	if err != nil {
		code := classifyErr(err)
		res, _ := failure(code, connectMessage(code, vm, err), vmTarget(vm))
		return nil, nil, nil, res
	}
	return runner, transfer, closeFn, nil
}

// ensureRemoteBase makes sure the remote base exists (unless create is
// false — sync's dry-run case, where creating anything would contradict
// "no writes happen"), then resolves its canonical real path fresh, since
// nothing is cached between calls and the remote directory structure
// could have changed since the last one.
func ensureRemoteBase(transfer sshlayer.FileTransfer, remoteBase string, create bool) (canonical string, existed bool, err error) {
	_, existed, err = transfer.Stat(remoteBase)
	if err != nil {
		return "", false, err
	}
	if !existed {
		if !create {
			return "", false, nil
		}
		if err := transfer.MkdirAll(remoteBase, remoteDirMode); err != nil {
			return "", false, err
		}
		existed = true
	}
	real, err := transfer.RealPath(remoteBase)
	if err != nil {
		return "", existed, err
	}
	return real, existed, nil
}
