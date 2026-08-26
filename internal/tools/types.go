// Package tools implements the five MCP tools: host_list, path, exec,
// push, sync. Each handler builds its CallToolResult manually rather than
// using the SDK's generic AddTool — see envelope.go's doc comment for why.
package tools

import (
	"ssh-mcp-light/internal/config"
	"ssh-mcp-light/internal/sshlayer"
)

// Deps are the dependencies shared by every tool handler, wired once at
// startup and never re-read: the server binds to one project and one
// vm.toml for its whole lifetime, so there's nothing to refresh.
type Deps struct {
	VMConfig      *config.VMConfig
	ProjectConfig *config.ProjectConfig
	Connector     sshlayer.VMConnector
}

// ResolvedTarget is echoed on every success and error response, so a
// caller can see exactly which VM/path an operation targeted or
// attempted — including on failure, which is why this is built and
// attached even to error paths rather than only to successful ones.
// Deliberately excludes the VM's address/port/user/identity file: those
// are connection credentials from vm.toml, not something the agent needs
// or should see — the VM name is the only handle exposed back to it.
type ResolvedTarget struct {
	VM         *string `json:"vm"`
	RemoteBase string  `json:"remote_base,omitempty"`
	Cwd        string  `json:"cwd,omitempty"`
}

func noVMTarget() ResolvedTarget { return ResolvedTarget{VM: nil} }

func vmTarget(vm config.VM) ResolvedTarget {
	name := vm.Name
	return ResolvedTarget{VM: &name}
}

func vmRemoteBaseTarget(vm config.VM, remoteBase string) ResolvedTarget {
	t := vmTarget(vm)
	t.RemoteBase = remoteBase
	return t
}

func vmCwdTarget(vm config.VM, cwd string) ResolvedTarget {
	t := vmTarget(vm)
	t.Cwd = cwd
	return t
}

// ErrorEnvelope is the exact JSON shape of every tool-level failure.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is the `error` object inside ErrorEnvelope.
type ErrorBody struct {
	Code           string         `json:"code"`
	Message        string         `json:"message"`
	ResolvedTarget ResolvedTarget `json:"resolved_target"`
}
