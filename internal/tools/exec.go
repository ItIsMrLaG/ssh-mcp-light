package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	execDefaultTimeoutSeconds = 60
	execMinTimeoutSeconds     = 1
	execMaxTimeoutSeconds     = 600
)

type execInput struct {
	VM             string   `json:"vm"`
	Cmd            string   `json:"cmd"`
	Args           []string `json:"args,omitempty"`
	Cwd            *string  `json:"cwd,omitempty"`
	TimeoutSeconds *int     `json:"timeout_seconds,omitempty"`
}

type execOutput struct {
	Stdout          string         `json:"stdout"`
	Stderr          string         `json:"stderr"`
	ExitCode        int            `json:"exit_code"`
	TimedOut        bool           `json:"timed_out"`
	TruncatedStdout bool           `json:"truncated_stdout"`
	TruncatedStderr bool           `json:"truncated_stderr"`
	ResolvedTarget  ResolvedTarget `json:"resolved_target"`
}

var execInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"vm":              map[string]any{"type": "string"},
		"cmd":             map[string]any{"type": "string", "minLength": 1},
		"args":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "default": []string{}},
		"cwd":             map[string]any{"type": "string"},
		"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600},
	},
	"required":             []string{"vm", "cmd"},
	"additionalProperties": false,
}

var execOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"stdout":           map[string]any{"type": "string"},
		"stderr":           map[string]any{"type": "string"},
		"exit_code":        map[string]any{"type": "integer"},
		"timed_out":        map[string]any{"type": "boolean"},
		"truncated_stdout": map[string]any{"type": "boolean"},
		"truncated_stderr": map[string]any{"type": "boolean"},
		"resolved_target":  map[string]any{"type": "object"},
	},
	"required": []string{"stdout", "stderr", "exit_code", "timed_out",
		"truncated_stdout", "truncated_stderr", "resolved_target"},
	"additionalProperties": false,
}

// RegisterExec registers the exec tool. It performs no path resolution,
// confinement check, or argument filtering — unlike push and sync, exec
// exists precisely to run arbitrary commands anywhere on the VM, so
// confining it would break its purpose. This is intended behavior, not a
// gap: don't add filtering here.
func RegisterExec(server *mcp.Server, deps *Deps) {
	tool := &mcp.Tool{
		Name:         "exec",
		Description:  "Run a command with arguments on a named VM; unrestricted, not path-confined.",
		InputSchema:  execInputSchema,
		OutputSchema: execOutputSchema,
	}
	server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in execInput
		if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
			return invalidArgument("(body)", err.Error())
		}
		if in.VM == "" {
			return invalidArgument("vm", "required, must be non-empty")
		}
		if in.Cmd == "" {
			return invalidArgument("cmd", "required, must be non-empty")
		}
		timeoutSeconds := execDefaultTimeoutSeconds
		if in.TimeoutSeconds != nil {
			timeoutSeconds = *in.TimeoutSeconds
			if timeoutSeconds < execMinTimeoutSeconds || timeoutSeconds > execMaxTimeoutSeconds {
				return invalidArgument("timeout_seconds", "must be between 1 and 600")
			}
		}

		vm, ok := lookupVM(deps.VMConfig, in.VM)
		if !ok {
			return failure(codeUnknownVM, unknownVM(in.VM), noVMTarget())
		}

		runner, _, closeFn, failRes := connectOrFail(ctx, deps.Connector, vm)
		if failRes != nil {
			return failRes, nil
		}
		defer func() { _ = closeFn() }()

		cwd := vm.RemoteRoot
		if in.Cwd != nil && *in.Cwd != "" {
			cwd = *in.Cwd
		}

		result, err := runner.Run(ctx, in.Cmd, in.Args, cwd, time.Duration(timeoutSeconds)*time.Second)
		if err != nil {
			code := classifyErr(err)
			return failure(code, err.Error(), vmCwdTarget(vm, cwd))
		}

		return success(execOutput{
			Stdout:          string(result.Stdout),
			Stderr:          string(result.Stderr),
			ExitCode:        result.ExitCode,
			TimedOut:        result.TimedOut,
			TruncatedStdout: result.TruncatedStdout,
			TruncatedStderr: result.TruncatedStderr,
			ResolvedTarget:  vmCwdTarget(vm, cwd),
		})
	})
}
