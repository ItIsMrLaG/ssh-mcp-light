package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ProjectConfig is the parsed, validated contents of the project config
// file, plus the resolved project root and its canonical form.
type ProjectConfig struct {
	// ProjectRoot is what path() and resolved_target echo back to callers:
	// absolute, but not symlink-resolved, so it stays recognizable as the
	// path the operator actually configured.
	ProjectRoot string
	// ProjectRootCanonical is the symlink-resolved form of ProjectRoot,
	// computed once here rather than on every push/sync call. It exists
	// only so confinement checks compare real paths — never shown to a
	// caller, because a symlinked ancestor shouldn't leak into responses.
	ProjectRootCanonical string

	RemoteLocalRoot string
	Include         []string
	Ignore          []string
	UseGitignore    bool
}

type projectTOML struct {
	ProjectRoot     string   `toml:"project_root"`
	RemoteLocalRoot string   `toml:"remote_local_root"`
	Include         []string `toml:"include"`
	Ignore          []string `toml:"ignore"`
	UseGitignore    bool     `toml:"use_gitignore"`
}

// LoadProjectConfig loads and validates the project config file named by
// path. As with LoadVMConfig, the returned error omits the "project config
// "<path>":" prefix — main.go adds it once, at the top level.
func LoadProjectConfig(path string) (*ProjectConfig, []string, error) {
	var raw projectTOML
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, nil, fmt.Errorf("%w", err)
	}

	if raw.ProjectRoot == "" {
		return nil, nil, fmt.Errorf("project_root: required, must be non-empty")
	}
	if raw.RemoteLocalRoot == "" {
		return nil, nil, fmt.Errorf("remote_local_root: required, must be non-empty")
	}
	if filepath.IsAbs(raw.RemoteLocalRoot) || strings.HasPrefix(raw.RemoteLocalRoot, "/") {
		return nil, nil, fmt.Errorf("remote_local_root: must be a relative path, got %q", raw.RemoteLocalRoot)
	}
	if hasDotDotSegment(raw.RemoteLocalRoot) {
		return nil, nil, fmt.Errorf("remote_local_root: must not contain a \"..\" segment, got %q", raw.RemoteLocalRoot)
	}
	for _, inc := range raw.Include {
		if filepath.IsAbs(inc) || strings.HasPrefix(inc, "/") {
			return nil, nil, fmt.Errorf("include: entry %q must be a relative path", inc)
		}
		if hasDotDotSegment(inc) {
			return nil, nil, fmt.Errorf("include: entry %q must not contain a \"..\" segment", inc)
		}
	}

	// A relative project_root resolves against the config file's own
	// directory, not the process's cwd or the config file's name — so the
	// config file can live anywhere (including outside the project
	// entirely) and moving it changes no resolved path as long as its
	// relative value is adjusted to match.
	configAbs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("project_root: could not resolve config file path: %w", err)
	}
	configDir := filepath.Dir(configAbs)

	projectRoot := raw.ProjectRoot
	if !filepath.IsAbs(projectRoot) {
		projectRoot = filepath.Join(configDir, projectRoot)
	}
	projectRoot = filepath.Clean(projectRoot)

	info, statErr := os.Stat(projectRoot)
	if statErr != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("project_root: resolved path %q does not exist or is not a directory", projectRoot)
	}

	canonical, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("project_root: could not resolve symlinks in %q: %v", projectRoot, err)
	}

	var warnings []string
	entries, err := os.ReadDir(projectRoot)
	if err == nil && len(entries) == 0 {
		warnings = append(warnings, fmt.Sprintf("project root %q is currently empty", projectRoot))
	}
	if raw.UseGitignore {
		if _, err := os.Stat(filepath.Join(projectRoot, ".gitignore")); err != nil {
			warnings = append(warnings, fmt.Sprintf("use_gitignore is true but %q contains no .gitignore file", projectRoot))
		}
	}

	return &ProjectConfig{
		ProjectRoot:          projectRoot,
		ProjectRootCanonical: canonical,
		RemoteLocalRoot:      filepath.ToSlash(filepath.Clean(raw.RemoteLocalRoot)),
		Include:              raw.Include,
		Ignore:               raw.Ignore,
		UseGitignore:         raw.UseGitignore,
	}, warnings, nil
}

func hasDotDotSegment(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// RemoteBase returns the project's remote directory on vm: its
// remote_root joined with this project's remote_local_root, so several
// projects can share one VM without their files colliding.
func (p *ProjectConfig) RemoteBase(vm VM) string {
	return RemoteJoin(vm.RemoteRoot, p.RemoteLocalRoot)
}

// RemoteJoin joins remote path segments with forward slashes regardless
// of the host OS — the remote side is always POSIX, even when this server
// runs on Windows.
func RemoteJoin(elem ...string) string {
	return filepath.ToSlash(filepath.Join(elem...))
}
