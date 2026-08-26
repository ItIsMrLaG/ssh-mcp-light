package tools

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"

	"ssh-mcp-light/internal/ignore"
	"ssh-mcp-light/internal/pathsafe"
	"ssh-mcp-light/internal/syncengine"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pushInput struct {
	VM    string   `json:"vm"`
	Files []string `json:"files"`
	Dest  string   `json:"dest,omitempty"`
}

type pushFailure struct {
	File      string `json:"file"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

type pushOutput struct {
	Uploaded        []string       `json:"uploaded"`
	SkippedByIgnore []string       `json:"skipped_by_ignore"`
	Failed          []pushFailure  `json:"failed"`
	ResolvedTarget  ResolvedTarget `json:"resolved_target"`
}

var pushInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"vm": map[string]any{"type": "string"},
		"files": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "string", "minLength": 1},
			"minItems": 1,
		},
		"dest": map[string]any{"type": "string", "default": "./"},
	},
	"required":             []string{"vm", "files"},
	"additionalProperties": false,
}

var pushOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"uploaded":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"skipped_by_ignore": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"failed": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file":       map[string]any{"type": "string"},
					"error_code": map[string]any{"type": "string"},
					"message":    map[string]any{"type": "string"},
				},
				"required":             []string{"file", "error_code", "message"},
				"additionalProperties": false,
			},
		},
		"resolved_target": map[string]any{"type": "object"},
	},
	"required":             []string{"uploaded", "skipped_by_ignore", "failed", "resolved_target"},
	"additionalProperties": false,
}

// RegisterPush registers the push tool.
func RegisterPush(server *mcp.Server, deps *Deps) {
	tool := &mcp.Tool{
		Name:         "push",
		Description:  "Copy specific files from the project root to a path under a VM's remote base, honoring ignore rules.",
		InputSchema:  pushInputSchema,
		OutputSchema: pushOutputSchema,
	}
	server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in pushInput
		if err := json.Unmarshal(req.Params.Arguments, &in); err != nil {
			return invalidArgument("(body)", err.Error())
		}
		if in.VM == "" {
			return invalidArgument("vm", "required, must be non-empty")
		}
		if len(in.Files) == 0 {
			return invalidArgument("files", "must have at least one entry")
		}
		dest := in.Dest
		if dest == "" {
			dest = "./"
		}

		vm, ok := lookupVM(deps.VMConfig, in.VM)
		if !ok {
			// Unlike a bad individual file below, an unknown VM aborts the
			// whole call: there's no meaningful partial result to compute
			// against a target that doesn't exist.
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

		remoteBaseCanonical, _, err := ensureRemoteBase(transfer, remoteBase, true)
		if err != nil {
			code := classifyErr(err)
			return failure(code, err.Error(), target)
		}

		matcher, err := ignore.New(pc.ProjectRoot, pc.Ignore, pc.UseGitignore)
		if err != nil {
			return failure(codeSFTPFailed, err.Error(), target)
		}

		out := pushOutput{
			Uploaded:        []string{},
			SkippedByIgnore: []string{},
			Failed:          []pushFailure{},
			ResolvedTarget:  target,
		}

		for _, file := range in.Files {
			// files is exempt from `include` — the caller named these
			// files explicitly, so restricting them further would defeat
			// the point of listing them — but still subject to `ignore`,
			// so a caller can't accidentally push build artifacts or logs
			// by naming them directly.
			slashFile := filepath.ToSlash(file)
			if matcher.Ignored(slashFile, false) {
				out.SkippedByIgnore = append(out.SkippedByIgnore, file)
				continue
			}

			localAbs, err := pathsafe.CheckLocalFile(pc.ProjectRoot, pc.ProjectRootCanonical, file, filepath.EvalSymlinks)
			if err != nil {
				out.Failed = append(out.Failed, pushFailure{File: file, ErrorCode: codePathTraversal, Message: err.Error()})
				continue
			}
			info, statErr := os.Stat(localAbs)
			if statErr != nil || info.IsDir() {
				out.Failed = append(out.Failed, pushFailure{File: file, ErrorCode: codeFileNotFound,
					Message: "file \"" + file + "\" not found under project root \"" + pc.ProjectRoot + "\""})
				continue
			}

			destRel := path.Join(dest, slashFile)
			remoteAbs, err := pathsafe.CheckRemoteLexical(remoteBase, destRel)
			if err != nil {
				out.Failed = append(out.Failed, pushFailure{File: file, ErrorCode: codePathTraversal, Message: err.Error()})
				continue
			}

			remoteDir := path.Dir(remoteAbs)
			if err := transfer.MkdirAll(remoteDir, remoteDirMode); err != nil {
				out.Failed = append(out.Failed, pushFailure{File: file, ErrorCode: classifyErr(err), Message: err.Error()})
				continue
			}
			real, err := transfer.RealPath(remoteDir)
			if err == nil {
				err = pathsafe.CheckRemoteReal(remoteBaseCanonical, real)
			}
			if err != nil {
				code := codePathTraversal
				if !isTraversal(err) {
					code = classifyErr(err)
				}
				out.Failed = append(out.Failed, pushFailure{File: file, ErrorCode: code, Message: err.Error()})
				continue
			}

			if err := transfer.Upload(localAbs, remoteAbs, syncengine.UploadMode(info.Mode())); err != nil {
				out.Failed = append(out.Failed, pushFailure{File: file, ErrorCode: classifyErr(err), Message: err.Error()})
				continue
			}
			out.Uploaded = append(out.Uploaded, path.Clean(destRel))
		}

		return success(out)
	})
}

func isTraversal(err error) bool {
	return classifyErr(err) == codePathTraversal
}
