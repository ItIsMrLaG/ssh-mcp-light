package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validVMToml = `
[vms.staging]
address = "10.0.4.12"
user = "deploy"
identity_file = "key"
remote_root = "/srv/agents"
`

const validProjectToml = `
project_root = "proj"
remote_local_root = "x"
`

// T-NO-PROJECT-FLAG.
func TestLoadConfigs_NoProjectFlag(t *testing.T) {
	var buf bytes.Buffer
	deps, code := loadConfigs(nil, &buf)
	if deps != nil || code != 1 {
		t.Fatalf("deps=%v code=%d, want nil/1", deps, code)
	}
	if !strings.Contains(buf.String(), "fatal: --project <path> is required") {
		t.Fatalf("stderr = %q", buf.String())
	}
}

// T-PROJECT-UNREADABLE.
func TestLoadConfigs_ProjectUnreadable(t *testing.T) {
	var buf bytes.Buffer
	deps, code := loadConfigs([]string{"--project", "/does/not/exist.toml", "--vm-config", "/x"}, &buf)
	if deps != nil || code != 1 {
		t.Fatalf("deps=%v code=%d, want nil/1", deps, code)
	}
	if !strings.Contains(buf.String(), "is not a readable file") {
		t.Fatalf("stderr = %q", buf.String())
	}
}

// T-NO-VM-CONFIG.
func TestLoadConfigs_NoVMConfig(t *testing.T) {
	dir := t.TempDir()
	projPath := filepath.Join(dir, "myproject.toml")
	write(t, projPath, validProjectToml)
	write(t, filepath.Join(dir, "proj", ".keep"), "")

	t.Setenv("VMMCP_CONFIG", "")
	var buf bytes.Buffer
	deps, code := loadConfigs([]string{"--project", projPath}, &buf)
	if deps != nil || code != 1 {
		t.Fatalf("deps=%v code=%d, want nil/1", deps, code)
	}
	if !strings.Contains(buf.String(), "VM config not set") {
		t.Fatalf("stderr = %q", buf.String())
	}
}

// T-VM-CONFIG-PRECEDENCE: --vm-config overrides $VMMCP_CONFIG.
func TestLoadConfigs_VMConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	projPath := filepath.Join(dir, "myproject.toml")
	write(t, projPath, validProjectToml)
	write(t, filepath.Join(dir, "proj", ".keep"), "")

	flagVMConfig := filepath.Join(dir, "flag-vm.toml")
	write(t, flagVMConfig, validVMToml)
	envVMConfig := filepath.Join(dir, "env-vm.toml")
	write(t, envVMConfig, "[vms]\n") // valid but empty, to tell them apart

	t.Setenv("VMMCP_CONFIG", envVMConfig)
	var buf bytes.Buffer
	deps, code := loadConfigs([]string{"--project", projPath, "--vm-config", flagVMConfig}, &buf)
	if code != 0 || deps == nil {
		t.Fatalf("code=%d deps=%v, stderr=%q", code, deps, buf.String())
	}
	if len(deps.VMConfig.VMs) != 1 {
		t.Fatalf("expected the flag's VM config (1 VM) to win over the env var's (0 VMs), got %d", len(deps.VMConfig.VMs))
	}
}

// T-PROJECT-ROOT-MISSING via the full startup path.
func TestLoadConfigs_ProjectRootMissing(t *testing.T) {
	dir := t.TempDir()
	projPath := filepath.Join(dir, "myproject.toml")
	write(t, projPath, "project_root = \"nope\"\nremote_local_root = \"x\"\n")
	vmPath := filepath.Join(dir, "vm.toml")
	write(t, vmPath, validVMToml)

	var buf bytes.Buffer
	deps, code := loadConfigs([]string{"--project", projPath, "--vm-config", vmPath}, &buf)
	if deps != nil || code != 1 {
		t.Fatalf("deps=%v code=%d, want nil/1", deps, code)
	}
	if !strings.Contains(buf.String(), `fatal: project config`) {
		t.Fatalf("stderr = %q", buf.String())
	}
}

// T-VM-CONFIG-EMPTY: zero VMs declared — server would start
// (loadConfigs succeeds), with a warning logged.
func TestLoadConfigs_VMConfigEmptyWarnsButSucceeds(t *testing.T) {
	dir := t.TempDir()
	projPath := filepath.Join(dir, "myproject.toml")
	write(t, projPath, validProjectToml)
	write(t, filepath.Join(dir, "proj", ".keep"), "")
	vmPath := filepath.Join(dir, "vm.toml")
	write(t, vmPath, "[vms]\n")

	var buf bytes.Buffer
	deps, code := loadConfigs([]string{"--project", projPath, "--vm-config", vmPath}, &buf)
	if code != 0 || deps == nil {
		t.Fatalf("code=%d deps=%v, stderr=%q", code, deps, buf.String())
	}
	if !strings.Contains(buf.String(), "warn:") {
		t.Fatalf("expected a warning to be logged, stderr=%q", buf.String())
	}
}

func TestLoadConfigs_Success(t *testing.T) {
	dir := t.TempDir()
	projPath := filepath.Join(dir, "myproject.toml")
	write(t, projPath, validProjectToml)
	write(t, filepath.Join(dir, "proj", ".keep"), "")
	vmPath := filepath.Join(dir, "vm.toml")
	write(t, vmPath, validVMToml)

	var buf bytes.Buffer
	deps, code := loadConfigs([]string{"--project", projPath, "--vm-config", vmPath}, &buf)
	if code != 0 || deps == nil {
		t.Fatalf("code=%d deps=%v, stderr=%q", code, deps, buf.String())
	}
	if deps.ProjectConfig.RemoteLocalRoot != "x" {
		t.Fatalf("unexpected project config: %+v", deps.ProjectConfig)
	}
}
