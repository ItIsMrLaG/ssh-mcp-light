package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadVMConfig_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantErr string // substring, empty means no error
	}{
		{
			name: "valid two VMs, declaration order preserved",
			toml: `
[vms.staging]
address = "10.0.4.12"
user = "deploy"
identity_file = "keys/staging"
remote_root = "/srv/agents"

[vms.build-1]
address = "build-1.internal"
user = "ci"
identity_file = "keys/build"
remote_root = "/home/ci/work"
`,
		},
		{
			name: "missing address",
			toml: `
[vms.staging]
user = "deploy"
identity_file = "keys/staging"
remote_root = "/srv/agents"
`,
			wantErr: "vms.staging.address",
		},
		{
			name: "missing user",
			toml: `
[vms.staging]
address = "10.0.4.12"
identity_file = "keys/staging"
remote_root = "/srv/agents"
`,
			wantErr: "vms.staging.user",
		},
		{
			name: "missing identity_file",
			toml: `
[vms.staging]
address = "10.0.4.12"
user = "deploy"
remote_root = "/srv/agents"
`,
			wantErr: "vms.staging.identity_file",
		},
		{
			name: "missing remote_root",
			toml: `
[vms.staging]
address = "10.0.4.12"
user = "deploy"
identity_file = "keys/staging"
`,
			wantErr: "vms.staging.remote_root",
		},
		{
			name: "remote_root not absolute",
			toml: `
[vms.staging]
address = "10.0.4.12"
user = "deploy"
identity_file = "keys/staging"
remote_root = "srv/agents"
`,
			wantErr: "vms.staging.remote_root",
		},
		{
			name: "port out of range",
			toml: `
[vms.staging]
address = "10.0.4.12"
port = 70000
user = "deploy"
identity_file = "keys/staging"
remote_root = "/srv/agents"
`,
			wantErr: "vms.staging.port",
		},
		{
			name: "bad VM name pattern",
			toml: `
[vms."bad name!"]
address = "10.0.4.12"
user = "deploy"
identity_file = "keys/staging"
remote_root = "/srv/agents"
`,
			wantErr: "name must match",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "vm.toml")
			writeFile(t, path, tc.toml)

			cfg, _, err := LoadVMConfig(path)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg == nil || len(cfg.VMs) == 0 {
					t.Fatalf("expected VMs to be loaded")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// T-VM-CONFIG-EMPTY: zero VMs declared — server starts, warns, host_list
// returns [].
func TestLoadVMConfig_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vm.toml")
	writeFile(t, path, "[vms]\n")

	cfg, warnings, err := LoadVMConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.VMs) != 0 {
		t.Fatalf("expected zero VMs, got %d", len(cfg.VMs))
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "zero VMs") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'zero VMs' warning, got %v", warnings)
	}
}

func TestLoadVMConfig_DeclarationOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vm.toml")
	writeFile(t, path, `
[vms.zeta]
address = "a"
user = "u"
identity_file = "k"
remote_root = "/r"

[vms.alpha]
address = "a"
user = "u"
identity_file = "k"
remote_root = "/r"
`)
	cfg, _, err := LoadVMConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"zeta", "alpha"}
	if len(cfg.Order) != 2 || cfg.Order[0] != want[0] || cfg.Order[1] != want[1] {
		t.Fatalf("expected declaration order %v, got %v", want, cfg.Order)
	}
}

// T-KEY relative identity_file resolves against vm.toml's own directory.
func TestLoadVMConfig_IdentityFileRelativeToVMConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vm.toml")
	writeFile(t, path, `
[vms.staging]
address = "a"
user = "u"
identity_file = "keys/staging_ed25519"
remote_root = "/r"
`)
	cfg, _, err := LoadVMConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "keys", "staging_ed25519")
	if cfg.VMs["staging"].IdentityFile != want {
		t.Fatalf("got %q, want %q", cfg.VMs["staging"].IdentityFile, want)
	}
}
