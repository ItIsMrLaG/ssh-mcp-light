package tools

import (
	"context"
	"encoding/json"

	"ssh-mcp-light/internal/sshlayer"
	"ssh-mcp-light/internal/syncengine"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type syncInput struct {
	VM     string `json:"vm"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type syncFailure struct {
	Path      string `json:"path"`
	Action    string `json:"action"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

type syncOutput struct {
	DryRun            bool           `json:"dry_run"`
	ToUpload          []string       `json:"to_upload"`
	ToDelete          []string       `json:"to_delete"`
	Uploaded          []string       `json:"uploaded"`
	Deleted           []string       `json:"deleted"`
	SkippedSymlinks   []string       `json:"skipped_symlinks"`
	ProtectedByIgnore []string       `json:"protected_by_ignore"`
	Failed            []syncFailure  `json:"failed"`
	ResolvedTarget    ResolvedTarget `json:"resolved_target"`
}

var syncInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"vm":      map[string]any{"type": "string"},
		"dry_run": map[string]any{"type": "boolean", "default": false},
	},
	"required":             []string{"vm"},
	"additionalProperties": false,
}

var syncOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"dry_run":             map[string]any{"type": "boolean"},
		"to_upload":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"to_delete":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"uploaded":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"deleted":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"skipped_symlinks":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"protected_by_ignore": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"failed": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string"},
					"action":     map[string]any{"type": "string", "enum": []string{"upload", "delete", "mkdir"}},
					"error_code": map[string]any{"type": "string"},
					"message":    map[string]any{"type": "string"},
				},
				"required":             []string{"path", "action", "error_code", "message"},
				"additionalProperties": false,
			},
		},
		"resolved_target": map[string]any{"type": "object"},
	},
	"required": []string{"dry_run", "to_upload", "to_delete", "uploaded", "deleted",
		"skipped_symlinks", "protected_by_ignore", "failed", "resolved_target"},
	"additionalProperties": false,
}

// RegisterSync registers the sync tool. Planning (BuildPlan) and
// execution (Apply) live in internal/syncengine; this handler is
// connection setup plus response shaping around them.
func RegisterSync(server *mcp.Server, deps *Deps) {
	tool := &mcp.Tool{
		Name:         "sync",
		Description:  "Mirror the project root to a VM's remote base: upload changed files, delete remote files with no local counterpart, skip symlinks, honor ignore rules.",
		InputSchema:  syncInputSchema,
		OutputSchema: syncOutputSchema,
	}
	server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in syncInput
		if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
			return invalidArgument("(body)", err.Error())
		}
		if in.VM == "" {
			return invalidArgument("vm", "required, must be non-empty")
		}

		vm, ok := lookupVM(deps.VMConfig, in.VM)
		if !ok {
			return failure(codeUnknownVM, unknownVM(in.VM), noVMTarget())
		}

		_, transfer, closeFn, failRes := connectOrFail(ctx, deps.Connector, vm)
		if failRes != nil {
			return failRes, nil
		}
		defer func() { _ = closeFn() }()

		pc := deps.ProjectConfig
		remoteBase := pc.RemoteBase(vm)
		target := vmRemoteBaseTarget(vm, remoteBase)

		remoteBaseCanonical, existed, err := ensureRemoteBase(transfer, remoteBase, !in.DryRun)
		if err != nil {
			return failure(classifyErr(err), err.Error(), target)
		}

		var remoteEntries []sshlayer.FileInfo
		if existed {
			remoteEntries, err = transfer.ReadDirRecursive(remoteBase)
			if err != nil {
				return failure(classifyErr(err), err.Error(), target)
			}
		}

		plan, err := syncengine.BuildPlan(pc.ProjectRoot, remoteEntries, pc.Include, pc.Ignore, pc.UseGitignore)
		if err != nil {
			return failure(codeSFTPFailed, err.Error(), target)
		}

		out := syncOutput{
			DryRun:            in.DryRun,
			ToUpload:          nonNil(pathsFromUploads(plan.ToUpload)),
			ToDelete:          nonNil(pathsFromDeletes(plan.ToDelete)),
			Uploaded:          []string{},
			Deleted:           []string{},
			SkippedSymlinks:   nonNil(plan.SkippedSymlinks),
			ProtectedByIgnore: nonNil(plan.ProtectedByIgnore),
			Failed:            []syncFailure{},
			ResolvedTarget:    target,
		}

		if in.DryRun {
			return success(out)
		}

		result := syncengine.Apply(transfer, remoteBase, remoteBaseCanonical, plan)
		out.Uploaded = nonNil(result.Uploaded)
		out.Deleted = nonNil(result.Deleted)
		for _, f := range result.Failed {
			out.Failed = append(out.Failed, syncFailure{Path: f.Path, Action: f.Action, ErrorCode: f.ErrorCode, Message: f.Message})
		}
		return success(out)
	})
}

func pathsFromUploads(u []syncengine.PlannedUpload) []string {
	out := make([]string, 0, len(u))
	for _, x := range u {
		out = append(out, x.RelPath)
	}
	return out
}

func pathsFromDeletes(d []syncengine.PlannedDelete) []string {
	out := make([]string, 0, len(d))
	for _, x := range d {
		out = append(out, x.RelPath)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
