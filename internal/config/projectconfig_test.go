package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectConfig_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		toml     string
		makeDirs []string // relative to a fresh temp dir, created before loading
		wantErr  string
	}{
		{
			name:     "valid, project_root relative",
			toml:     "project_root = \"proj\"\nremote_local_root = \"projects/api\"\n",
			makeDirs: []string{"proj"},
		},
		{
			// T-PROJECT-ROOT-MISSING
			name:    "project_root does not exist",
			toml:    "project_root = \"nope\"\nremote_local_root = \"x\"\n",
			wantErr: "does not exist",
		},
		{
			// T-REMOTE-LOCAL-ROOT-ABSOLUTE
			name:     "remote_local_root absolute",
			toml:     "project_root = \"proj\"\nremote_local_root = \"/abs\"\n",
			makeDirs: []string{"proj"},
			wantErr:  "remote_local_root",
		},
		{
			// T-REMOTE-LOCAL-ROOT-DOTDOT
			name:     "remote_local_root contains dotdot",
			toml:     "project_root = \"proj\"\nremote_local_root = \"a/../../b\"\n",
			makeDirs: []string{"proj"},
			wantErr:  "remote_local_root",
		},
		{
			name:    "missing project_root",
			toml:    "remote_local_root = \"x\"\n",
			wantErr: "project_root",
		},
		{
			name:    "missing remote_local_root",
			toml:    "project_root = \".\"\n",
			wantErr: "remote_local_root",
		},
		{
			name:     "include entry absolute rejected",
			toml:     "project_root = \"proj\"\nremote_local_root = \"x\"\ninclude = [\"/etc\"]\n",
			makeDirs: []string{"proj"},
			wantErr:  "include",
		},
		{
			name:     "include entry with dotdot rejected",
			toml:     "project_root = \"proj\"\nremote_local_root = \"x\"\ninclude = [\"../escape\"]\n",
			makeDirs: []string{"proj"},
			wantErr:  "include",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range tc.makeDirs {
				writeFile(t, filepath.Join(dir, d, ".keep"), "")
			}
			cfgPath := filepath.Join(dir, "myproject.toml")
			writeFile(t, cfgPath, tc.toml)

			pc, _, err := LoadProjectConfig(cfgPath)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if pc.ProjectRoot == "" {
					t.Fatalf("expected a resolved project root")
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

// T-PROJECT-ROOT-RELATIVE-TO-CONFIG-DIR: two config files at different
// filesystem locations, each with a relative project_root re-expressed to
// point at the same target directory, resolve to the same <project-root>.
func TestLoadProjectConfig_RelativeResolutionIndependentOfConfigLocation(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "work", "api")
	writeFile(t, filepath.Join(target, ".keep"), "")

	// Config A: sits directly next to the target's parent.
	cfgA := filepath.Join(base, "work", "a.toml")
	writeFile(t, cfgA, "project_root = \"api\"\nremote_local_root = \"x\"\n")

	// Config B: sits two levels deeper elsewhere, with a relative path
	// re-expressed to reach the very same target directory.
	cfgBDir := filepath.Join(base, "deploy", "nested")
	cfgB := filepath.Join(cfgBDir, "b.toml")
	writeFile(t, cfgB, "project_root = \"../../work/api\"\nremote_local_root = \"x\"\n")

	pcA, _, errA := LoadProjectConfig(cfgA)
	pcB, _, errB := LoadProjectConfig(cfgB)
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v / %v", errA, errB)
	}
	if pcA.ProjectRoot != pcB.ProjectRoot {
		t.Fatalf("resolved project roots differ: %q vs %q", pcA.ProjectRoot, pcB.ProjectRoot)
	}
}

func TestLoadProjectConfig_CanonicalFollowsSymlinkedAncestor(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeFile(t, filepath.Join(real, "proj", ".keep"), "")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	cfgPath := filepath.Join(link, "myproject.toml")
	writeFile(t, cfgPath, "project_root = \"proj\"\nremote_local_root = \"x\"\n")

	pc, _, err := LoadProjectConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCanonical := filepath.Join(real, "proj")
	if pc.ProjectRootCanonical != wantCanonical {
		t.Fatalf("canonical = %q, want %q", pc.ProjectRootCanonical, wantCanonical)
	}
	// The uncanonicalized form still goes through the symlink.
	if pc.ProjectRoot != filepath.Join(link, "proj") {
		t.Fatalf("project root = %q, want the symlinked form", pc.ProjectRoot)
	}
}
