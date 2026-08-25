package sshlayer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path"

	"github.com/pkg/sftp"
)

type sftpTransfer struct {
	client *sftp.Client
}

func toFileInfo(relPath string, fi os.FileInfo) FileInfo {
	return FileInfo{
		Path:    relPath,
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
		IsLink:  fi.Mode()&os.ModeSymlink != 0,
	}
}

// Stat implements FileTransfer.
func (t *sftpTransfer) Stat(remotePath string) (FileInfo, bool, error) {
	fi, err := t.client.Lstat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return FileInfo{}, false, nil
		}
		return FileInfo{}, false, &SFTPError{Err: err}
	}
	return toFileInfo(path.Base(remotePath), fi), true, nil
}

// ReadDirRecursive implements FileTransfer. It does not follow symlinks:
// a symlinked directory is reported as an entry but never descended into,
// so sync can never plan a write or delete through one.
func (t *sftpTransfer) ReadDirRecursive(remotePath string) ([]FileInfo, error) {
	var out []FileInfo
	var walk func(dir, relPrefix string) error
	walk = func(dir, relPrefix string) error {
		entries, err := t.client.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return &SFTPError{Err: err}
		}
		for _, e := range entries {
			rel := e.Name()
			if relPrefix != "" {
				rel = relPrefix + "/" + rel
			}
			info := toFileInfo(rel, e)
			out = append(out, info)
			if info.IsDir && !info.IsLink {
				if err := walk(path.Join(dir, e.Name()), rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(remotePath, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// MkdirAll implements FileTransfer. Unlike sftp.Client.MkdirAll, it chmods
// every directory it creates, not just the leaf — the upstream call
// leaves intermediate directories at the server's default mode, which
// would make a synced tree's permissions depend on the remote server's
// umask instead of being predictable.
func (t *sftpTransfer) MkdirAll(remotePath string, mode os.FileMode) error {
	if remotePath == "" || remotePath == "/" || remotePath == "." {
		return nil
	}
	if fi, err := t.client.Stat(remotePath); err == nil {
		if fi.IsDir() {
			return nil
		}
		return &SFTPError{Err: fmt.Errorf("%s exists and is not a directory", remotePath)}
	}
	parent := path.Dir(remotePath)
	if parent != remotePath {
		if err := t.MkdirAll(parent, mode); err != nil {
			return err
		}
	}
	if err := t.client.Mkdir(remotePath); err != nil {
		if fi, statErr := t.client.Stat(remotePath); statErr == nil && fi.IsDir() {
			return nil // tolerate a concurrent creator
		}
		return &SFTPError{Err: err}
	}
	if err := t.client.Chmod(remotePath, mode); err != nil {
		return &SFTPError{Err: err}
	}
	return nil
}

// RealPath implements FileTransfer.
func (t *sftpTransfer) RealPath(remotePath string) (string, error) {
	rp, err := t.client.RealPath(remotePath)
	if err != nil {
		return "", &SFTPError{Err: err}
	}
	return rp, nil
}

// Remove implements FileTransfer.
func (t *sftpTransfer) Remove(remotePath string) error {
	if err := t.client.Remove(remotePath); err != nil {
		return &SFTPError{Err: err}
	}
	return nil
}

// RemoveDir implements FileTransfer.
func (t *sftpTransfer) RemoveDir(remotePath string) error {
	if err := t.client.RemoveDirectory(remotePath); err != nil {
		return &SFTPError{Err: err}
	}
	return nil
}

// Rename implements FileTransfer. Plain SFTPv3 RENAME fails outright if
// newPath already exists, so an overwrite needs either the
// posix-rename@openssh.com extension (tried first — atomic even when
// newPath exists, what real OpenSSH supports) or, as a last resort for a
// server offering neither, a remove-then-rename with a small non-atomic
// window between the two. A brand-new upload (nothing at newPath yet)
// is always atomic via the second branch alone.
func (t *sftpTransfer) Rename(oldPath, newPath string) error {
	if err := t.client.PosixRename(oldPath, newPath); err == nil {
		return nil
	}
	if err := t.client.Rename(oldPath, newPath); err == nil {
		return nil
	}
	_ = t.client.Remove(newPath)
	if err := t.client.Rename(oldPath, newPath); err != nil {
		return &SFTPError{Err: err}
	}
	return nil
}

// tempName builds the atomic-upload temporary name:
// .<basename>.<8 lowercase hex digits>.tmp, in the same directory as
// destPath so the eventual rename is same-directory (and therefore atomic
// on a POSIX filesystem, not a cross-device copy).
func tempName(destPath string) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	dir, base := path.Split(destPath)
	return path.Join(dir, fmt.Sprintf(".%s.%s.tmp", base, hex.EncodeToString(buf))), nil
}

// Upload implements FileTransfer: write to a temporary file in the
// destination directory, then rename into place, so a reader can never
// observe a partially-written file under the final name. On any failure
// before the rename completes, the temporary file is removed on a
// best-effort basis.
func (t *sftpTransfer) Upload(localPath, remotePath string, mode os.FileMode) error {
	tmp, err := tempName(remotePath)
	if err != nil {
		return &SFTPError{Err: err}
	}

	local, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = local.Close() }()

	remote, err := t.client.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return &SFTPError{Err: err}
	}
	if err := remote.Chmod(mode); err != nil {
		_ = remote.Close()
		_ = t.client.Remove(tmp)
		return &SFTPError{Err: err}
	}

	if _, err := remote.ReadFrom(local); err != nil {
		_ = remote.Close()
		_ = t.client.Remove(tmp)
		return &SFTPError{Err: err}
	}
	if err := remote.Close(); err != nil {
		_ = t.client.Remove(tmp)
		return &SFTPError{Err: err}
	}

	if err := t.Rename(tmp, remotePath); err != nil {
		_ = t.client.Remove(tmp)
		return err
	}
	return nil
}
