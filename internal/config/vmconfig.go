// Package config parses and validates vm.toml and the project config
// file, and resolves <project-root> and its canonical form.
package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// VM is one validated entry from vm.toml.
type VM struct {
	Name         string
	Address      string
	Port         int
	User         string
	IdentityFile string // resolved against vm.toml's own directory, not the process cwd, if given relative
	RemoteRoot   string
	Description  string
}

// VMConfig is the parsed, validated contents of vm.toml.
type VMConfig struct {
	VMs   map[string]VM
	Order []string // declaration order, so callers can list VMs the way the operator wrote them
}

// Lookup is the single choke point every tool funnels a caller-supplied VM
// name through, so "unknown VM" is rejected the same way everywhere.
func (c *VMConfig) Lookup(name string) (VM, bool) {
	vm, ok := c.VMs[name]
	return vm, ok
}

var vmNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type vmTOML struct {
	Address      string `toml:"address"`
	Port         *int64 `toml:"port"`
	User         string `toml:"user"`
	IdentityFile string `toml:"identity_file"`
	RemoteRoot   string `toml:"remote_root"`
	Description  string `toml:"description"`
}

type vmFileTOML struct {
	VMs map[string]vmTOML `toml:"vms"`
}

// LoadVMConfig loads and validates vm.toml. The returned error, if
// non-nil, deliberately omits the "VM config "<path>":" prefix — main.go
// adds that once, at the top level, so every fatal startup message has one
// consistent shape regardless of which loader produced it.
func LoadVMConfig(path string) (*VMConfig, []string, error) {
	var raw vmFileTOML
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%w", err)
	}
	if raw.VMs == nil {
		return nil, nil, fmt.Errorf(`top level lacks a "vms" table`)
	}

	baseDir := filepath.Dir(mustAbs(path))

	out := &VMConfig{VMs: make(map[string]VM, len(raw.VMs))}
	var warnings []string

	// md.Keys() (not raw.VMs, a Go map) preserves file order — Go map
	// iteration is randomized, and host_list must list VMs in the order
	// the operator declared them.
	seen := make(map[string]bool)
	for _, k := range md.Keys() {
		if len(k) != 2 || k[0] != "vms" {
			continue
		}
		name := k[1]
		if seen[name] {
			continue
		}
		seen[name] = true

		v, ok := raw.VMs[name]
		if !ok {
			continue
		}
		if !vmNamePattern.MatchString(name) {
			return nil, nil, fmt.Errorf("vms.%s: name must match %s", name, vmNamePattern.String())
		}
		if v.Address == "" {
			return nil, nil, fmt.Errorf("vms.%s.address: required, must be non-empty", name)
		}
		if v.User == "" {
			return nil, nil, fmt.Errorf("vms.%s.user: required, must be non-empty", name)
		}
		if v.IdentityFile == "" {
			return nil, nil, fmt.Errorf("vms.%s.identity_file: required, must be non-empty", name)
		}
		if v.RemoteRoot == "" {
			return nil, nil, fmt.Errorf("vms.%s.remote_root: required, must be non-empty", name)
		}
		if !strings.HasPrefix(v.RemoteRoot, "/") {
			return nil, nil, fmt.Errorf("vms.%s.remote_root: must be an absolute remote path (start with \"/\"), got %q", name, v.RemoteRoot)
		}
		port := int64(22)
		if v.Port != nil {
			port = *v.Port
		}
		if port < 1 || port > 65535 {
			return nil, nil, fmt.Errorf("vms.%s.port: must be between 1 and 65535, got %d", name, port)
		}

		identityFile := v.IdentityFile
		if !filepath.IsAbs(identityFile) {
			identityFile = filepath.Join(baseDir, identityFile)
		}

		if v.Description == "" {
			warnings = append(warnings, fmt.Sprintf("vm.toml: vms.%s.description is empty", name))
		}

		out.VMs[name] = VM{
			Name:         name,
			Address:      v.Address,
			Port:         int(port),
			User:         v.User,
			IdentityFile: identityFile,
			RemoteRoot:   v.RemoteRoot,
			Description:  v.Description,
		}
		out.Order = append(out.Order, name)
	}

	if len(out.VMs) == 0 {
		warnings = append(warnings, "vm.toml declares zero VMs")
	}

	return out, warnings, nil
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
