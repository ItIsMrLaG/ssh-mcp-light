// Package mcpserver wires the five tools onto an MCP server instance.
package mcpserver

import (
	"ssh-mcp-light/internal/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the server's reported implementation version.
const Version = "0.1.0"

// New builds an MCP server with all five tools registered.
func New(deps *tools.Deps) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ssh-mcp-light", Version: Version}, nil)
	tools.RegisterHostList(server, deps)
	tools.RegisterPath(server, deps)
	tools.RegisterExec(server, deps)
	tools.RegisterPush(server, deps)
	tools.RegisterSync(server, deps)
	return server
}
