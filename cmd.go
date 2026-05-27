package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Non-interactive subcommands — designed for AI agents and shell scripts.
//
//   wtkill list [--json] [filters]   list worktrees
//   wtkill rm <path>... [--force]    remove specific worktree(s)
//   wtkill clean [filters] [--dry-run] [--force]   bulk remove by filter
//
// All subcommands exit 0 on success, non-zero on partial/total failure.
// JSON output is auto-enabled when stdout is not a TTY.
// ─────────────────────────────────────────────────────────────────────────────

type jsonWorktree struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	Head      string `json:"head,omitempty"`
	Detached  bool   `json:"detached,omitempty"`
	Bare      bool   `json:"bare,omitempty"`
	Prunable  bool   `json:"prunable,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	AgeDays   int    `json:"age_days"`
}

type listResult struct {
	Root         string         `json:"root"`
	ScannedRepos int            `json:"scanned_repos"`
	ElapsedMs    int64          `json:"elapsed_ms"`
	TotalBytes   int64          `json:"total_bytes"`
	Count        int            `json:"count"`
	Worktrees    []jsonWorktree `json:"worktrees"`
}

type rmResult struct {
	Path  string `json:"path"`
	Repo  string `json:"repo,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type cleanResult struct {
	DryRun     bool       `json:"dry_run"`
	Matched    int        `json:"matched"`
	Removed    int        `json:"removed"`
	Failed     int        `json:"failed"`
	FreedBytes int64      `json:"freed_bytes"`
	Items      []rmResult `json:"items"`
}

// filters used by list and clean
type filters struct {
	olderThan    time.Duration
	hasOlderThan bool
	branchRegex  *regexp.Regexp
	prunableOnly bool
	missingOnly  bool
	minSize      int64
}

func parseFlags(args []string) (root string, depth int, force, includeMain, jsonOut, dryRun bool, f filters, positional []string, err error) {
	root = ""
	depth = 4
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			i++
			if i >= len(args) {
				err = fmt.Errorf("flag %s requires a value", a)
				return ""
			}
			return args[i]
		}
		switch {
		case a == "--path":
			root = next()
		case a == "--depth":
			d, e := strconv.Atoi(next())
			if e != nil {
				err = fmt.Errorf("--depth: %w", e)
				return
			}
			depth = d
		case a == "--force":
			force = true
		case a == "--include-main":
			includeMain = true
		case a == "--json":
			jsonOut = true
		case a == "--dry-run":
			dryRun = true
		case a == "--older-than":
			d, e := parseDuration(next())
			if e != nil {
				err = e
				return
			}
			f.olderThan = d
			f.hasOlderThan = true
		case a == "--branch":
			re, e := regexp.Compile(next())
			if e != nil {
				err = fmt.Errorf("--branch regex: %w", e)
				return
			}
			f.branchRegex = re
		case a == "--prunable":
			f.prunableOnly = true
		case a == "--missing":
			f.missingOnly = true
		case a == "--min-size":
			s, e := parseSize(next())
			if e != nil {
				err = e
				return
			}
			f.minSize = s
		case strings.HasPrefix(a, "--"):
			err = fmt.Errorf("unknown flag: %s", a)
			return
		default:
			positional = append(positional, a)
		}
	}
	return
}

func parseDuration(s string) (time.Duration, error) {
	// Accept "30d", "12h", "7d12h" — Go's time.ParseDuration doesn't accept days.
	var total time.Duration
	cur := ""
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			cur += string(r)
			continue
		}
		if cur == "" {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		val, err := strconv.ParseFloat(cur, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		var unit time.Duration
		switch r {
		case 'd':
			unit = 24 * time.Hour
		case 'h':
			unit = time.Hour
		case 'm':
			unit = time.Minute
		case 's':
			unit = time.Second
		case 'w':
			unit = 7 * 24 * time.Hour
		default:
			return 0, fmt.Errorf("unknown duration unit %q (use s/m/h/d/w)", r)
		}
		total += time.Duration(val * float64(unit))
		cur = ""
	}
	if cur != "" {
		return 0, fmt.Errorf("duration %q missing unit", s)
	}
	return total, nil
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	mul := int64(1)
	upper := strings.ToUpper(s)
	suffixes := []struct {
		s string
		m int64
	}{
		{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1},
	}
	for _, sx := range suffixes {
		if strings.HasSuffix(upper, sx.s) {
			s = strings.TrimSuffix(upper, sx.s)
			mul = sx.m
			break
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}
	return int64(v * float64(mul)), nil
}

func (f filters) match(wt Worktree) bool {
	if f.prunableOnly && !wt.Prunable {
		return false
	}
	if f.missingOnly && !wt.Missing {
		return false
	}
	if f.branchRegex != nil && !f.branchRegex.MatchString(wt.Branch) {
		return false
	}
	if f.minSize > 0 && wt.Size < f.minSize {
		return false
	}
	if f.hasOlderThan {
		minDays := int(f.olderThan.Hours() / 24)
		if wt.AgeDays < 0 || wt.AgeDays < minDays {
			return false
		}
	}
	return true
}

// collectAll runs the scanner synchronously and returns all worktrees.
func collectAll(root string, depth int, includeMain bool) ([]Worktree, int, time.Duration) {
	start := time.Now()
	ch := make(chan any, 256)
	done := make(chan struct{})
	var items []Worktree
	var scanned int

	go func() {
		for msg := range ch {
			switch m := msg.(type) {
			case worktreeFoundMsg:
				items = append(items, m.wt)
			case scanProgressMsg:
				scanned = m.scanned
			case scanDoneMsg:
				close(done)
				return
			}
		}
	}()

	startScan(root, depth, includeMain, ch)
	<-done
	return items, scanned, time.Since(start)
}

func toJSON(wt Worktree) jsonWorktree {
	return jsonWorktree{
		Repo:      wt.Repo,
		Path:      wt.Path,
		Branch:    wt.Branch,
		Head:      wt.Head,
		Detached:  wt.Detached,
		Bare:      wt.Bare,
		Prunable:  wt.Prunable,
		Missing:   wt.Missing,
		SizeBytes: wt.Size,
		AgeDays:   wt.AgeDays,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// list
// ─────────────────────────────────────────────────────────────────────────────
func cmdList(args []string) int {
	root, depth, _, includeMain, jsonOut, _, f, _, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wtkill list:", err)
		return 2
	}
	if root == "" {
		root = defaultRoot()
	}
	if !isTTY() {
		jsonOut = true
	}

	items, scanned, elapsed := collectAll(root, depth, includeMain)
	var filtered []Worktree
	var total int64
	for _, it := range items {
		if !f.match(it) {
			continue
		}
		filtered = append(filtered, it)
		total += it.Size
	}

	if jsonOut {
		res := listResult{
			Root:         root,
			ScannedRepos: scanned,
			ElapsedMs:    elapsed.Milliseconds(),
			TotalBytes:   total,
			Count:        len(filtered),
			Worktrees:    make([]jsonWorktree, 0, len(filtered)),
		}
		for _, it := range filtered {
			res.Worktrees = append(res.Worktrees, toJSON(it))
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return 0
	}

	// plain text
	fmt.Printf("%-50s  %-40s  %10s  %5s  %s\n", "BRANCH", "REPO", "SIZE", "AGE", "PATH")
	for _, it := range filtered {
		branch := it.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Printf("%-50s  %-40s  %10s  %5s  %s\n",
			truncate(branch, 50),
			truncate(lastTwoSegments(it.Repo), 40),
			fmtSize(it.Size),
			fmtAge(it.AgeDays),
			it.Path,
		)
	}
	fmt.Printf("\n%d worktrees · %s · scanned %d repos in %.1fs\n",
		len(filtered), fmtSize(total), scanned, elapsed.Seconds())
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// rm — remove specific worktree paths
// ─────────────────────────────────────────────────────────────────────────────
func cmdRm(args []string) int {
	_, _, force, _, jsonOut, _, _, paths, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wtkill rm:", err)
		return 2
	}
	if !isTTY() {
		jsonOut = true
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "wtkill rm: at least one path required")
		return 2
	}

	results := make([]rmResult, 0, len(paths))
	failed := 0
	for _, p := range paths {
		repo, e := findRepoForWorktree(p)
		r := rmResult{Path: p, Repo: repo}
		if e != nil {
			r.OK = false
			r.Error = e.Error()
			failed++
		} else {
			ok, errMsg := removeWorktree(repo, p, force)
			r.OK = ok
			if !ok {
				r.Error = errMsg
				failed++
			}
		}
		results = append(results, r)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Results []rmResult `json:"results"`
			Failed  int        `json:"failed"`
		}{Results: results, Failed: failed})
	} else {
		for _, r := range results {
			if r.OK {
				fmt.Printf("removed  %s\n", r.Path)
			} else {
				fmt.Printf("FAILED   %s — %s\n", r.Path, r.Error)
			}
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// findRepoForWorktree resolves the parent repo of a worktree path
// using `git -C <path> rev-parse --git-common-dir`.
func findRepoForWorktree(path string) (string, error) {
	out, err := runCmd("git", "-C", path, "rev-parse", "--show-toplevel")
	if err == nil && out != "" {
		// To get the *main* repo (not the worktree itself), use git-common-dir
		common, err2 := runCmd("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err2 == nil && common != "" {
			// strip trailing /.git or /.git/worktrees/...
			common = strings.TrimSpace(common)
			if i := strings.LastIndex(common, "/.git"); i > 0 {
				return common[:i], nil
			}
			return strings.TrimSpace(out), nil
		}
		return strings.TrimSpace(out), nil
	}
	return "", fmt.Errorf("not a git worktree: %s", path)
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// ─────────────────────────────────────────────────────────────────────────────
// clean — bulk remove by filters
// ─────────────────────────────────────────────────────────────────────────────
func cmdClean(args []string) int {
	root, depth, force, includeMain, jsonOut, dryRun, f, _, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wtkill clean:", err)
		return 2
	}
	if root == "" {
		root = defaultRoot()
	}
	if !isTTY() {
		jsonOut = true
	}

	// safety: require at least one filter so `wtkill clean` doesn't nuke everything
	if !f.hasOlderThan && f.branchRegex == nil && !f.prunableOnly && !f.missingOnly && f.minSize == 0 {
		fmt.Fprintln(os.Stderr, "wtkill clean: at least one filter required (--older-than, --branch, --prunable, --missing, --min-size)")
		return 2
	}

	items, _, _ := collectAll(root, depth, includeMain)
	var matched []Worktree
	for _, it := range items {
		if f.match(it) {
			matched = append(matched, it)
		}
	}

	res := cleanResult{DryRun: dryRun, Matched: len(matched), Items: make([]rmResult, 0, len(matched))}
	for _, it := range matched {
		r := rmResult{Path: it.Path, Repo: it.Repo}
		if dryRun {
			r.OK = true
		} else {
			ok, errMsg := removeWorktree(it.Repo, it.Path, force)
			r.OK = ok
			if !ok {
				r.Error = errMsg
				res.Failed++
			} else {
				res.Removed++
				res.FreedBytes += it.Size
			}
		}
		res.Items = append(res.Items, r)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		action := "removed"
		if dryRun {
			action = "would remove"
		}
		for _, r := range res.Items {
			if r.OK {
				fmt.Printf("%s  %s\n", action, r.Path)
			} else {
				fmt.Printf("FAILED       %s — %s\n", r.Path, r.Error)
			}
		}
		fmt.Printf("\n%d matched · %d removed · %d failed · freed %s\n",
			res.Matched, res.Removed, res.Failed, fmtSize(res.FreedBytes))
	}
	if res.Failed > 0 {
		return 1
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// utils
// ─────────────────────────────────────────────────────────────────────────────
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func defaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home + "/Projects"
}
