package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type vmInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type hostListOutput struct {
	VMs []vmInfo `json:"vms"`
}

var hostListInputSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
}

var hostListOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"vms": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
				"required":             []string{"name", "description"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"vms"},
	"additionalProperties": false,
}

// RegisterHostList registers the host_list tool.
func RegisterHostList(server *mcp.Server, deps *Deps) {
	tool := &mcp.Tool{
		Name:         "host_list",
		Description:  "Return the VMs this project may target, with the fields an agent needs to choose one.",
		InputSchema:  hostListInputSchema,
		OutputSchema: hostListOutputSchema,
	}
	server.AddTool(tool, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		out := hostListOutput{VMs: []vmInfo{}}
		for _, name := range deps.VMConfig.Order {
			vm := deps.VMConfig.VMs[name]
			out.VMs = append(out.VMs, vmInfo{Name: vm.Name, Description: vm.Description})
		}
		return success(out)
	})
}
