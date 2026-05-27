package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Worktree is one row in the list.
type Worktree struct {
	Repo      string
	Path      string
	Branch    string
	Head      string
	Detached  bool
	Bare      bool
	Prunable  bool
	Missing   bool
	Size      int64
	AgeDays   int
	Status    string // "", "deleting", "deleted", "failed"
	StatusErr string
}

var skipDirs = map[string]struct{}{
	"node_modules": {}, ".next": {}, "dist": {}, "build": {}, ".cache": {},
	"vendor": {}, ".venv": {}, "venv": {}, "__pycache__": {}, ".turbo": {},
	"target": {}, ".gradle": {}, "Pods": {}, "DerivedData": {},
	".idea": {}, ".vscode": {},
}

// scanProgress is emitted as each repo is being processed.
type scanProgressMsg struct {
	scanned int
	current string
}

// worktreeFoundMsg is emitted when a worktree gets enriched and is ready to display.
type worktreeFoundMsg struct {
	wt Worktree
}

type scanDoneMsg struct {
	elapsed time.Duration
}

// startScan walks the filesystem and pushes results into a channel.
// The bubbletea program reads from the channel through readChannelCmd.
func startScan(root string, depth int, includeMain bool, out chan<- any) {
	start := time.Now()
	var scanned int
	var mu sync.Mutex

	// Phase 1: find repos
	repos := findRepos(root, depth)

	// Phase 1b: dedupe by canonical git common dir — otherwise repos that share
	// a .git via symlink (e.g. ~/Projects/foo and ~/Projects/team/foo) report
	// the same worktrees twice. After we remove one, the other reports stale
	// entries that fail to delete. Deduping at scan time avoids the confusion.
	repos = dedupeReposByGitDir(repos)

	// Phase 2: for each repo concurrently enrich its worktrees
	sem := make(chan struct{}, 8) // limit parallel `du`
	var wg sync.WaitGroup

	for _, repo := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			scanned++
			out <- scanProgressMsg{scanned: scanned, current: repo}
			mu.Unlock()

			wts := listWorktrees(repo)
			start := 0
			if !includeMain {
				start = 1
			}
			if start >= len(wts) {
				return
			}
			for _, wt := range wts[start:] {
				wt.Repo = repo
				if _, err := os.Stat(wt.Path); err != nil {
					wt.Missing = true
				} else {
					wt.Size = dirSize(wt.Path)
					wt.AgeDays = lastCommitDays(wt.Path)
				}
				out <- worktreeFoundMsg{wt: wt}
			}
		}(repo)
	}

	wg.Wait()
	out <- scanDoneMsg{elapsed: time.Since(start)}
}

func dedupeReposByGitDir(repos []string) []string {
	seen := make(map[string]bool, len(repos))
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		key := canonicalGitDir(r)
		if key == "" {
			out = append(out, r) // keep entries we can't resolve
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

func canonicalGitDir(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func findRepos(root string, maxDepth int) []string {
	var repos []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		// Repo detection: has .git entry (file or dir)
		for _, e := range entries {
			if e.Name() == ".git" {
				repos = append(repos, dir)
				return
			}
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if _, skip := skipDirs[e.Name()]; skip {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth+1)
		}
	}
	walk(root, 0)
	return repos
}

func listWorktrees(repo string) []Worktree {
	cmd := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var result []Worktree
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var cur Worktree
	hasCur := false
	flush := func() {
		if hasCur {
			result = append(result, cur)
		}
		cur = Worktree{}
		hasCur = false
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		hasCur = true
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Detached = true
		case strings.HasPrefix(line, "prunable"):
			cur.Prunable = true
		}
	}
	flush()
	return result
}

func dirSize(path string) int64 {
	cmd := exec.Command("du", "-sk", path)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 {
		return 0
	}
	kb, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

func lastCommitDays(path string) int {
	cmd := exec.Command("git", "-C", path, "log", "-1", "--format=%ct")
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	tsStr := strings.TrimSpace(string(out))
	if tsStr == "" {
		return -1
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return -1
	}
	return int((time.Now().Unix() - ts) / 86400)
}

// deleteResultMsg is the outcome of a `git worktree remove` call.
type deleteResultMsg struct {
	idx int
	ok  bool
	err string
}

func removeWorktree(repo, path string, force bool) (bool, string) {
	args := []string{"-C", repo, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		// first line, truncated
		if i := strings.Index(msg, "\n"); i >= 0 {
			msg = msg[:i]
		}
		if len(msg) > 120 {
			msg = msg[:120]
		}
		return false, msg
	}
	// prune stale refs
	exec.Command("git", "-C", repo, "worktree", "prune").Run()
	return true, ""
}
