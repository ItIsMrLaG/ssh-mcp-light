// Command ssh-mcp-light is a stdio MCP server giving an LLM agent
// controlled SSH access to a fixed set of VMs declared in vm.toml.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"ssh-mcp-light/internal/config"
	"ssh-mcp-light/internal/mcpserver"
	"ssh-mcp-light/internal/sshlayer"
	"ssh-mcp-light/internal/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run resolves and validates configuration, then serves the MCP stdio
// transport. It returns the process exit code.
func run(args []string, stderr io.Writer) int {
	deps, code := loadConfigs(args, stderr)
	if deps == nil {
		return code
	}

	server := mcpserver.New(deps)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		_, _ = fmt.Fprintf(stderr, "server exited: %v\n", err)
		return 1
	}
	return 0
}

// loadConfigs does everything that must succeed before the server may
// accept a tool call. It is split out from run so tests can exercise
// startup validation without also starting the (blocking) stdio server.
// A nil *tools.Deps means the process must exit immediately with code.
func loadConfigs(args []string, stderr io.Writer) (*tools.Deps, int) {
	fs := flag.NewFlagSet("ssh-mcp-light", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFlag := fs.String("project", "", "path to the project config file (required)")
	vmConfigFlag := fs.String("vm-config", "", "path to vm.toml")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}

	// No fallback for --project by design: an environment variable,
	// directory walk, or default filename would let the server bind to
	// the wrong project silently depending on where it happened to be
	// launched from.
	if *projectFlag == "" {
		_, _ = fmt.Fprintln(stderr, "fatal: --project <path> is required")
		return nil, 1
	}
	if info, err := os.Stat(*projectFlag); err != nil || info.IsDir() {
		reason := err
		if reason == nil {
			reason = fmt.Errorf("is a directory")
		}
		_, _ = fmt.Fprintf(stderr, "fatal: project config %q is not a readable file: %v\n", *projectFlag, reason)
		return nil, 1
	}

	// --vm-config wins over $VMMCP_CONFIG when both are set, so a
	// one-off override doesn't require unsetting the environment.
	vmConfigPath := *vmConfigFlag
	if vmConfigPath == "" {
		vmConfigPath = os.Getenv("VMMCP_CONFIG")
	}
	if vmConfigPath == "" {
		_, _ = fmt.Fprintln(stderr, "fatal: VM config not set: pass --vm-config <path> or set VMMCP_CONFIG")
		return nil, 1
	}
	if info, err := os.Stat(vmConfigPath); err != nil || info.IsDir() {
		reason := err
		if reason == nil {
			reason = fmt.Errorf("is a directory")
		}
		_, _ = fmt.Fprintf(stderr, "fatal: VM config %q is not a readable file: %v\n", vmConfigPath, reason)
		return nil, 1
	}

	vmConfig, vmWarnings, err := config.LoadVMConfig(vmConfigPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fatal: VM config %q: %v\n", vmConfigPath, err)
		return nil, 1
	}
	for _, w := range vmWarnings {
		_, _ = fmt.Fprintf(stderr, "warn: %s\n", w)
	}

	projectConfig, projWarnings, err := config.LoadProjectConfig(*projectFlag)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fatal: project config %q: %v\n", *projectFlag, err)
		return nil, 1
	}
	for _, w := range projWarnings {
		_, _ = fmt.Fprintf(stderr, "warn: %s\n", w)
	}

	return &tools.Deps{
		VMConfig:      vmConfig,
		ProjectConfig: projectConfig,
		Connector:     sshlayer.NewConnector(),
	}, 0
}
