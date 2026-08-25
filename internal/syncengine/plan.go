// Package syncengine computes and executes sync's change set: which local
// files to upload, which remote-only entries to delete, which local
// entries to skip because they're symlinks or excluded, and which
// deletion candidates to protect instead of removing.
package syncengine

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ssh-mcp-light/internal/ignore"
	"ssh-mcp-light/internal/sshlayer"
)

// mtimeSlack absorbs mtime rounding across the SFTP protocol, so a file
// that's genuinely unchanged doesn't get re-uploaded purely because the
// remote server reported its mtime a second or two off from the local
// value.
const mtimeSlack = 2 * time.Second

// PlannedUpload is one local file selected for upload, keyed by its path
// relative to the project root.
type PlannedUpload struct {
	RelPath   string
	LocalPath string
}

// PlannedDelete is one remote-only entry selected for deletion, keyed by
// its path relative to the remote base, in leaves-first order (files
// before the directories that would contain them).
type PlannedDelete struct {
	RelPath string
	IsDir   bool
}

// Plan is the full computed change set for one sync call.
type Plan struct {
	ToUpload          []PlannedUpload
	ToDelete          []PlannedDelete
	SkippedSymlinks   []string
	ProtectedByIgnore []string
}

type localEntry struct {
	relPath string
	isDir   bool
	isLink  bool
	size    int64
	modTime time.Time
}

// walkLocal enumerates the project root using the real local filesystem
// directly — sync only ever mirrors the real local filesystem in
// production, so there's no abstraction to inject here; tests use real
// temporary directories instead of a fake.
func walkLocal(root string) ([]localEntry, error) {
	var out []localEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		isLink := info.Mode()&os.ModeSymlink != 0
		out = append(out, localEntry{
			relPath: rel,
			isDir:   d.IsDir(),
			isLink:  isLink,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		return nil
	})
	return out, err
}

// BuildPlan computes the change set. remoteEntries is the already-fetched
// recursive listing of the remote base (each Path relative to the remote
// base) rather than something BuildPlan fetches itself, so this function
// stays pure and testable with a canned listing instead of a live
// connection.
func BuildPlan(projectRoot string, remoteEntries []sshlayer.FileInfo, includePatterns, ignorePatterns []string, useGitignore bool) (*Plan, error) {
	matcher, err := ignore.New(projectRoot, ignorePatterns, useGitignore)
	if err != nil {
		return nil, err
	}

	localEntries, err := walkLocal(projectRoot)
	if err != nil {
		return nil, err
	}

	plan := &Plan{}
	localAll := map[string]bool{}
	localFiles := map[string]localEntry{}

	for _, e := range localEntries {
		if e.isLink {
			plan.SkippedSymlinks = append(plan.SkippedSymlinks, e.relPath)
			continue
		}
		if !ignore.MatchInclude(includePatterns, e.relPath) {
			continue
		}
		if matcher.Ignored(e.relPath, e.isDir) {
			continue
		}
		localAll[e.relPath] = true
		if !e.isDir {
			localFiles[e.relPath] = e
		}
	}

	remoteByPath := make(map[string]sshlayer.FileInfo, len(remoteEntries))
	for _, r := range remoteEntries {
		remoteByPath[r.Path] = r
	}

	// Size-and-mtime comparison, not a content hash: hashing would mean
	// reading every local file (and either downloading or remotely
	// hashing every remote file) on every single sync call, which doesn't
	// scale with project size. The trade-off this accepts: a same-size
	// edit whose mtime isn't newer than the remote's goes undetected.
	var uploadPaths []string
	for rel, e := range localFiles {
		remote, ok := remoteByPath[rel]
		needsUpload := !ok || remote.IsDir || remote.IsLink ||
			remote.Size != e.size ||
			e.modTime.Sub(remote.ModTime) > mtimeSlack
		if needsUpload {
			uploadPaths = append(uploadPaths, rel)
		}
	}
	sort.Strings(uploadPaths) // deterministic, diffable output across runs
	for _, rel := range uploadPaths {
		plan.ToUpload = append(plan.ToUpload, PlannedUpload{
			RelPath:   rel,
			LocalPath: filepath.Join(projectRoot, filepath.FromSlash(rel)),
		})
	}

	// A remote symlink is left alone either way — never a delete
	// candidate, never overwritten as a "changed" file — so it's excluded
	// from consideration here rather than filtered out later.
	var candidates []sshlayer.FileInfo
	for _, r := range remoteEntries {
		if r.IsLink {
			continue
		}
		if !localAll[r.Path] {
			candidates = append(candidates, r)
		}
	}

	// A deletion candidate that include/ignore would exclude if it
	// existed locally is protected, not deleted. Without this, adding an
	// ignore pattern like *.log after the fact would delete every
	// matching file already on the remote on the next sync — the exact
	// opposite of what an ignore pattern is for.
	var eligible []sshlayer.FileInfo
	for _, r := range candidates {
		if !ignore.MatchInclude(includePatterns, r.Path) || matcher.Ignored(r.Path, r.IsDir) {
			plan.ProtectedByIgnore = append(plan.ProtectedByIgnore, r.Path)
			continue
		}
		eligible = append(eligible, r)
	}
	sort.Strings(plan.ProtectedByIgnore)

	// A directory deletes only if nothing under it survives — including
	// protected entries, which count as surviving even though they won't
	// be deleted themselves.
	eligibleSet := make(map[string]bool, len(eligible))
	for _, r := range eligible {
		eligibleSet[r.Path] = true
	}
	var final []sshlayer.FileInfo
	for _, r := range eligible {
		if r.IsDir && hasSurvivingDescendant(r.Path, remoteEntries, eligibleSet) {
			continue
		}
		final = append(final, r)
	}

	// Deepest paths first, so a file is always removed before the
	// directory that (soon) contains it.
	sort.Slice(final, func(i, j int) bool {
		di, dj := strings.Count(final[i].Path, "/"), strings.Count(final[j].Path, "/")
		if di != dj {
			return di > dj
		}
		return final[i].Path < final[j].Path
	})
	for _, r := range final {
		plan.ToDelete = append(plan.ToDelete, PlannedDelete{RelPath: r.Path, IsDir: r.IsDir})
	}

	sort.Strings(plan.SkippedSymlinks)
	return plan, nil
}

func hasSurvivingDescendant(dirPath string, remoteEntries []sshlayer.FileInfo, eligibleSet map[string]bool) bool {
	prefix := dirPath + "/"
	for _, other := range remoteEntries {
		if other.IsLink || other.Path == dirPath {
			continue
		}
		if strings.HasPrefix(other.Path, prefix) && !eligibleSet[other.Path] {
			return true
		}
	}
	return false
}
