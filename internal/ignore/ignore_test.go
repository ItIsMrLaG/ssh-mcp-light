package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

// T-IGNORE-GLOB, T-IGNORE-DIR-TRAILING-SLASH, T-IGNORE-NEGATION,
// T-IGNORE-ANCHORED: one table-driven test per rule.
func TestMatcher_Ignored_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		path     string
		isDir    bool
		want     bool
	}{
		// T-IGNORE-GLOB
		{"glob matches suffix anywhere", []string{"*.log"}, "internal/testdata/fixture.log", false, true},
		{"glob does not match non-matching file", []string{"*.log"}, "internal/testdata/fixture.txt", false, false},
		// T-IGNORE-DIR-TRAILING-SLASH
		{"dir-only pattern matches a directory", []string{"tmp/"}, "tmp", true, true},
		{"dir-only pattern does not match a same-named file", []string{"tmp/"}, "tmp", false, false},
		// T-IGNORE-ANCHORED
		{"anchored pattern matches only at root", []string{"/build"}, "build", true, true},
		{"anchored pattern does not match nested dir of same name", []string{"/build"}, "sub/build", true, false},
		{"unanchored pattern matches at any depth", []string{"build"}, "sub/build", true, true},
		// T-IGNORE-NEGATION
		{"negation re-includes a specific file", []string{"*.log", "!keep.log"}, "keep.log", false, false},
		{"negation does not affect other matches", []string{"*.log", "!keep.log"}, "other.log", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := New(t.TempDir(), tc.patterns, false)
			if err != nil {
				t.Fatal(err)
			}
			got := m.Ignored(tc.path, tc.isDir)
			if got != tc.want {
				t.Fatalf("Ignored(%q, isDir=%v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
			}
		})
	}
}

// A directory-matching pattern excludes everything under it, even a file
// that would not itself match any pattern.
func TestMatcher_DirectoryExclusionInherited(t *testing.T) {
	m, err := New(t.TempDir(), []string{"logs/"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Ignored("logs", true) {
		t.Fatalf("expected logs/ itself to be ignored")
	}
	if !m.Ignored("logs/app.log", false) {
		t.Fatalf("expected logs/app.log to inherit exclusion from its ignored parent directory")
	}
	if m.Ignored("other/app.log", false) {
		t.Fatalf("other/app.log should not be affected")
	}
}

// T-IGNORE-INCLUDE-RESTRICTS-BEFORE-IGNORE.
func TestMatchInclude(t *testing.T) {
	cases := []struct {
		name     string
		includes []string
		path     string
		want     bool
	}{
		{"empty include means everything eligible", nil, "any/path.go", true},
		{"exact file match", []string{"go.mod"}, "go.mod", true},
		{"path inside an included directory", []string{"cmd"}, "cmd/api/main.go", true},
		{"path outside every included entry", []string{"cmd"}, "internal/foo.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchInclude(tc.includes, tc.path)
			if got != tc.want {
				t.Fatalf("MatchInclude(%v, %q) = %v, want %v", tc.includes, tc.path, got, tc.want)
			}
		})
	}
}

// T-IGNORE-GITIGNORE-PRECEDENCE: a pattern in a deeper .gitignore
// overrides one in a shallower one for paths under that deeper directory.
func TestMatcher_GitignorePrecedence(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(root, "keep"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "keep", ".gitignore"), []byte("!*.log\n"), 0o644))

	m, err := New(root, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Ignored("outer.log", false) {
		t.Fatalf("expected outer.log to be ignored by the root .gitignore")
	}
	if m.Ignored("keep/inner.log", false) {
		t.Fatalf("expected keep/inner.log to be re-included by the deeper .gitignore")
	}
}

// The project config's own `ignore` array is evaluated after (and can
// override) .gitignore patterns.
func TestMatcher_OwnIgnoreOverridesGitignore(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644))

	m, err := New(root, []string{"!important.log"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Ignored("important.log", false) {
		t.Fatalf("expected the project config's own ignore array to re-include important.log")
	}
	if !m.Ignored("other.log", false) {
		t.Fatalf("expected other.log to remain ignored")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
