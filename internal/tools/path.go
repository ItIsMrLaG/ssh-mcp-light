package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pathInput struct {
	VM *string `json:"vm,omitempty"`
}

type pathOutput struct {
	Path           string         `json:"path"`
	ResolvedTarget ResolvedTarget `json:"resolved_target"`
}

var pathInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"vm": map[string]any{"type": "string"},
	},
	"required":             []string{},
	"additionalProperties": false,
}

var pathOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":            map[string]any{"type": "string"},
		"resolved_target": map[string]any{"type": "object"},
	},
	"required":             []string{"path", "resolved_target"},
	"additionalProperties": false,
}

// RegisterPath registers the path tool: the tool an agent calls instead
// of guessing or hardcoding a local or remote path.
func RegisterPath(server *mcp.Server, deps *Deps) {
	tool := &mcp.Tool{
		Name:         "path",
		Description:  "Return a resolved absolute path: the project root with no argument, or a VM's remote base with one.",
		InputSchema:  pathInputSchema,
		OutputSchema: pathOutputSchema,
	}
	server.AddTool(tool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in pathInput
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
				return invalidArgument("vm", err.Error())
			}
		}
		if in.VM == nil || *in.VM == "" {
			return success(pathOutput{Path: deps.ProjectConfig.ProjectRoot, ResolvedTarget: noVMTarget()})
		}
		vm, ok := lookupVM(deps.VMConfig, *in.VM)
		if !ok {
			return failure(codeUnknownVM, unknownVM(*in.VM), noVMTarget())
		}
		// The remote base is pure config arithmetic — no network round
		// trip needed just to answer "where would this land".
		remoteBase := deps.ProjectConfig.RemoteBase(vm)
		return success(pathOutput{Path: remoteBase, ResolvedTarget: vmRemoteBaseTarget(vm, remoteBase)})
	})
}
