package syncengine

import (
	"path"

	"ssh-mcp-light/internal/errcodes"
	"ssh-mcp-light/internal/pathsafe"
	"ssh-mcp-light/internal/sshlayer"
)

// FailedAction is one entry of a tool response's `failed` array.
type FailedAction struct {
	Path      string
	Action    string // "upload" | "delete" | "mkdir"
	ErrorCode string
	Message   string
}

// ApplyResult is what actually happened when a Plan was executed. A
// planned action absent from both Uploaded/Deleted and Failed can't
// happen — every entry in plan.ToUpload/ToDelete ends up in exactly one
// of those, which is what lets a caller trust an empty Failed as "the
// whole plan succeeded."
type ApplyResult struct {
	Uploaded []string
	Deleted  []string
	Failed   []FailedAction
}

const uploadFileMode = 0o644
const dirMode = 0o755

// Apply executes plan against remoteBase over transfer. remoteBaseCanonical
// is resolved once by the caller, before Apply runs, and passed in fixed;
// Apply doesn't re-resolve the base itself, but before each delete it does
// re-resolve that *candidate's* own real path and re-checks it against the
// fixed base, so a symlink planted on the remote side after planning but
// before this delete runs still can't route a delete outside the base.
//
// A per-action failure doesn't stop the loop: every other planned action
// still gets attempted, and if the connection is genuinely dead, each
// subsequent attempt just fails the same way — with the same effect as an
// explicit "abort the rest" branch, but without one being needed.
func Apply(transfer sshlayer.FileTransfer, remoteBase, remoteBaseCanonical string, plan *Plan) *ApplyResult {
	result := &ApplyResult{}
	madeDirs := map[string]bool{}

	// Uploads before deletes, unconditionally: a delete never races an
	// upload for the same path.
	for _, u := range plan.ToUpload {
		remotePath := path.Join(remoteBase, u.RelPath)
		dir := path.Dir(remotePath)
		if dir != "" && dir != "." && !madeDirs[dir] {
			if err := transfer.MkdirAll(dir, dirMode); err != nil {
				result.Failed = append(result.Failed, FailedAction{
					Path: u.RelPath, Action: "mkdir",
					ErrorCode: classify(err), Message: err.Error(),
				})
				continue
			}
			madeDirs[dir] = true
		}
		if err := transfer.Upload(u.LocalPath, remotePath, uploadFileMode); err != nil {
			result.Failed = append(result.Failed, FailedAction{
				Path: u.RelPath, Action: "upload",
				ErrorCode: classify(err), Message: err.Error(),
			})
			continue
		}
		result.Uploaded = append(result.Uploaded, u.RelPath)
	}

	for _, d := range plan.ToDelete {
		remotePath := path.Join(remoteBase, d.RelPath)
		real, err := transfer.RealPath(remotePath)
		if err == nil {
			err = pathsafe.CheckRemoteReal(remoteBaseCanonical, real)
		}
		if err != nil {
			result.Failed = append(result.Failed, FailedAction{
				Path: d.RelPath, Action: "delete",
				ErrorCode: classify(err), Message: err.Error(),
			})
			continue
		}
		if d.IsDir {
			err = transfer.RemoveDir(remotePath)
		} else {
			err = transfer.Remove(remotePath)
		}
		if err != nil {
			result.Failed = append(result.Failed, FailedAction{
				Path: d.RelPath, Action: "delete",
				ErrorCode: classify(err), Message: err.Error(),
			})
			continue
		}
		result.Deleted = append(result.Deleted, d.RelPath)
	}

	return result
}

func classify(err error) string {
	return errcodes.Classify(err)
}
