// Package pathsafe implements the confinement checks push and sync use to
// keep every write and delete inside a fixed root, even when a symlinked
// directory tries to route them elsewhere. exec deliberately does not use
// this package: it runs unconfined by design, not by omission.
package pathsafe

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// ErrTraversal is wrapped by every rejection this package returns, so
// callers can classify the failure with errors.Is instead of string
// matching.
var ErrTraversal = errors.New("path traversal")

// splitRel rejects an absolute path or one containing a ".." segment.
func splitRel(relPath string) ([]string, error) {
	if relPath == "" {
		return nil, fmt.Errorf("%w: empty path", ErrTraversal)
	}
	slashed := filepath.ToSlash(relPath)
	if strings.HasPrefix(slashed, "/") {
		return nil, fmt.Errorf("%w: %q is absolute", ErrTraversal, relPath)
	}
	segs := strings.Split(slashed, "/")
	for _, s := range segs {
		if s == ".." {
			return nil, fmt.Errorf("%w: %q contains a \"..\" segment", ErrTraversal, relPath)
		}
	}
	return segs, nil
}

// CheckLocalLexical validates relPath against root by string comparison
// only — no filesystem access, so it's safe to call before the path is
// known to exist.
func CheckLocalLexical(root, relPath string) (string, error) {
	if _, err := splitRel(relPath); err != nil {
		return "", err
	}
	candidate := filepath.Clean(filepath.Join(root, relPath))
	if candidate != root && !strings.HasPrefix(candidate, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q resolves outside %q", ErrTraversal, relPath, root)
	}
	return candidate, nil
}

// EvalSymlinksFunc resolves a path's real, symlink-free form. Satisfied by
// filepath.EvalSymlinks; parameterized so tests can inject a fake instead
// of touching the real filesystem.
type EvalSymlinksFunc func(string) (string, error)

// CheckLocalFile validates a push `files` entry end-to-end: lexical
// confinement to root, then symlink-resolved confinement of the file's
// real parent directory to rootCanonical. The two-step check matters
// because a symlink *inside* root that points elsewhere would pass the
// lexical check alone.
func CheckLocalFile(root, rootCanonical, relPath string, evalSymlinks EvalSymlinksFunc) (string, error) {
	candidate, err := CheckLocalLexical(root, relPath)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(candidate)
	realParent, err := evalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("%w: could not resolve real parent of %q: %v", ErrTraversal, relPath, err)
	}
	if realParent != rootCanonical && !strings.HasPrefix(realParent, rootCanonical+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q resolves outside project root (real parent %q, canonical root %q)", ErrTraversal, relPath, realParent, rootCanonical)
	}
	return candidate, nil
}

// CheckRemoteLexical validates relPath against the remote base by string
// comparison only, using POSIX path arithmetic — the remote side is
// always POSIX regardless of the host this server runs on.
func CheckRemoteLexical(remoteBase, relPath string) (string, error) {
	if _, err := splitRel(relPath); err != nil {
		return "", err
	}
	candidate := path.Clean(path.Join(remoteBase, relPath))
	if candidate != remoteBase && !strings.HasPrefix(candidate, remoteBase+"/") {
		return "", fmt.Errorf("%w: %q resolves outside remote base %q", ErrTraversal, relPath, remoteBase)
	}
	return candidate, nil
}

// CheckRemoteReal validates that realPath — the remote's own resolution of
// a directory that already passed CheckRemoteLexical, or is about to be
// deleted — is inside remoteBaseCanonical. This is the remote-side
// counterpart to CheckLocalFile's real-parent check: a lexically-confined
// path can still escape through a remote symlink the lexical check can't
// see.
func CheckRemoteReal(remoteBaseCanonical, realPath string) error {
	realPath = path.Clean(realPath)
	if realPath != remoteBaseCanonical && !strings.HasPrefix(realPath, remoteBaseCanonical+"/") {
		return fmt.Errorf("%w: real remote path %q resolves outside remote base %q", ErrTraversal, realPath, remoteBaseCanonical)
	}
	return nil
}
