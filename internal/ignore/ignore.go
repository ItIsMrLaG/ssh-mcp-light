// Package ignore is a self-contained gitignore(5)-compatible pattern
// matcher: precedence is include, then nested .gitignore files
// (deeper overrides shallower), then the project's own ignore list last.
// Hand-rolled instead of importing go-git's gitignore package, which
// would pull in that project's whole dependency tree for one subpackage.
package ignore

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// pattern is one compiled gitignore-style line.
type pattern struct {
	negate   bool
	dirOnly  bool
	anchored bool
	segs     []string
}

func compilePattern(raw string) (pattern, bool) {
	trimmed := strings.TrimRight(raw, " ")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return pattern{}, false
	}
	p := trimmed
	neg := false
	if strings.HasPrefix(p, "!") {
		neg = true
		p = p[1:]
	}
	p = strings.TrimPrefix(p, `\`) // minimal escape handling for leading \! or \#
	dirOnly := strings.HasSuffix(p, "/")
	if dirOnly {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "" {
		return pattern{}, false
	}
	anchored := strings.HasPrefix(p, "/")
	p = strings.TrimPrefix(p, "/")
	segs := strings.Split(p, "/")
	if len(segs) > 1 {
		anchored = true
	}
	return pattern{negate: neg, dirOnly: dirOnly, anchored: anchored, segs: segs}, true
}

func matchSegs(pat, pth []string) bool {
	if len(pat) == 0 {
		return len(pth) == 0
	}
	if pat[0] == "**" {
		if len(pat) == 1 {
			return true
		}
		for i := 0; i <= len(pth); i++ {
			if matchSegs(pat[1:], pth[i:]) {
				return true
			}
		}
		return false
	}
	if len(pth) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], pth[0])
	if !ok {
		return false
	}
	return matchSegs(pat[1:], pth[1:])
}

func (p pattern) matches(relSegs []string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if p.anchored {
		return matchSegs(p.segs, relSegs)
	}
	for i := 0; i < len(relSegs); i++ {
		if matchSegs(p.segs, relSegs[i:]) {
			return true
		}
	}
	return false
}

func compileLines(lines []string) []pattern {
	var out []pattern
	for _, l := range lines {
		if p, ok := compilePattern(l); ok {
			out = append(out, p)
		}
	}
	return out
}

// applyPatterns runs the last-match-wins gitignore algorithm over patterns
// in order, starting from excluded=state.
func applyPatterns(patterns []pattern, relSegs []string, isDir bool, state bool) bool {
	for _, p := range patterns {
		if p.matches(relSegs, isDir) {
			state = !p.negate
		}
	}
	return state
}

// Matcher evaluates ignore/gitignore precedence for a project rooted at a
// fixed directory.
type Matcher struct {
	// gitignoreDirs is every directory (relative to <project-root>, ""
	// for the root itself) that has a loaded .gitignore, walked in
	// root-to-leaf order when a candidate path is matched.
	gitignoreDirs []string
	gitignoreBy   map[string][]pattern
	ownIgnore     []pattern
}

// New builds a Matcher. If useGitignore, every .gitignore file under
// projectRoot is loaded; ignorePatterns (the project config's own
// `ignore` list) is evaluated last, so it can override anything a
// .gitignore says.
func New(projectRoot string, ignorePatterns []string, useGitignore bool) (*Matcher, error) {
	m := &Matcher{
		gitignoreBy: make(map[string][]pattern),
		ownIgnore:   compileLines(ignorePatterns),
	}
	if useGitignore {
		err := filepath.WalkDir(projectRoot, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // best-effort; unreadable subtree is not a fatal ignore-load error
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != ".gitignore" {
				return nil
			}
			rel, err := filepath.Rel(projectRoot, filepath.Dir(p))
			if err != nil {
				return nil //nolint:nilerr
			}
			if rel == "." {
				rel = ""
			}
			rel = filepath.ToSlash(rel)
			f, err := os.Open(p)
			if err != nil {
				return nil //nolint:nilerr
			}
			defer func() { _ = f.Close() }()
			var lines []string
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			m.gitignoreBy[rel] = compileLines(lines)
			m.gitignoreDirs = append(m.gitignoreDirs, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
		sortByDepth(m.gitignoreDirs)
	}
	return m, nil
}

// sortByDepth sorts directories root-first (shallow to deep), so deeper
// .gitignore patterns are applied later in Ignored and win ties.
func sortByDepth(dirs []string) {
	less := func(i, j int) bool {
		return strings.Count(dirs[i], "/") < strings.Count(dirs[j], "/")
	}
	// small n; insertion sort keeps this dependency-free
	for i := 1; i < len(dirs); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			dirs[j], dirs[j-1] = dirs[j-1], dirs[j]
		}
	}
}

// Ignored reports whether relPath (slash-separated, relative to the
// project root) is excluded by .gitignore files and/or the project
// config's `ignore` array. It does not evaluate `include` — see
// MatchInclude.
//
// Every ancestor directory of relPath is checked first, so a directory
// match excludes everything under it even when relPath is tested on its
// own (as sync's remote-deletion check does — it isn't a top-down walk,
// so it can't rely on simply never descending into an excluded
// directory).
func (m *Matcher) Ignored(relPath string, isDir bool) bool {
	segs := strings.Split(relPath, "/")
	for i := 1; i < len(segs); i++ {
		if m.ignoredSelf(strings.Join(segs[:i], "/"), true) {
			return true
		}
	}
	return m.ignoredSelf(relPath, isDir)
}

// ignoredSelf evaluates relPath's own match state, without considering
// whether an ancestor directory is excluded.
func (m *Matcher) ignoredSelf(relPath string, isDir bool) bool {
	relSegs := strings.Split(relPath, "/")
	state := false
	for _, dir := range m.gitignoreDirs {
		if dir != "" && !strings.HasPrefix(relPath, dir+"/") {
			continue
		}
		sub := relSegs
		if dir != "" {
			sub = relSegs[len(strings.Split(dir, "/")):]
		}
		state = applyPatterns(m.gitignoreBy[dir], sub, isDir, state)
	}
	state = applyPatterns(m.ownIgnore, relSegs, isDir, state)
	return state
}

// MatchInclude reports whether relPath is, or is inside, one of the
// listed entries. An empty include list means everything is eligible —
// include is a restriction, not a requirement to enumerate one.
func MatchInclude(includes []string, relPath string) bool {
	if len(includes) == 0 {
		return true
	}
	for _, inc := range includes {
		inc = strings.Trim(filepath.ToSlash(inc), "/")
		if relPath == inc || strings.HasPrefix(relPath, inc+"/") {
			return true
		}
	}
	return false
}
