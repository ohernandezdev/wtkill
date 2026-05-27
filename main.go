package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	version = "0.3.0"
	author  = "Omar Hernández (@ohernandezdev)"
	repoURL = "https://github.com/ohernandezdev/wtkill"
)

func main() {
	args := os.Args[1:]

	// help
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			printHelp()
			return
		}
		if a == "--version" || a == "-V" {
			fmt.Printf("wtkill %s\nby %s\n%s\n", version, author, repoURL)
			return
		}
	}

	// subcommand dispatch
	if len(args) > 0 {
		switch args[0] {
		case "list", "ls":
			os.Exit(cmdList(args[1:]))
		case "rm", "remove":
			os.Exit(cmdRm(args[1:]))
		case "clean":
			os.Exit(cmdClean(args[1:]))
		}
	}

	// No subcommand → TUI if interactive, else default to `list --json` so
	// agents and scripts that invoke `wtkill` get structured output.
	if !isTTY() {
		os.Exit(cmdList(append([]string{"--json"}, args...)))
	}

	runTUI(args)
}

func runTUI(args []string) {
	usr, _ := user.Current()
	defaultRoot := filepath.Join(usr.HomeDir, "Projects")

	root := defaultRoot
	depth := 4
	force := false
	includeMain := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			i++
			if i < len(args) {
				root = args[i]
			}
		case "--depth":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &depth)
			}
		case "--force":
			force = true
		case "--include-main":
			includeMain = true
		}
	}
	root, _ = filepath.Abs(root)

	m := newModel(root, depth, force, includeMain)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wtkill error:", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`wtkill %s — find and clean git worktrees

USAGE
  wtkill                          interactive TUI (humans)
  wtkill list   [filters]         list worktrees as text
  wtkill rm     <path>...         remove specific worktree(s)
  wtkill clean  [filters]         bulk remove by filter

  When stdout is not a TTY (pipe / AI tool call), output is JSON by default.

GLOBAL FLAGS
  --path <dir>        scan root (default ~/Projects)
  --depth <n>         max scan depth (default 4)
  --include-main      include each repo's main worktree
  --force             pass --force to git worktree remove
  --json              force JSON output
  --version           print version

FILTERS  (for list and clean)
  --older-than <dur>  e.g. 30d, 12h, 2w   match worktrees older than this
  --branch <regex>    match branches by regex
  --prunable          only worktrees git marks prunable
  --missing           only worktrees whose path no longer exists
  --min-size <size>   e.g. 100MB, 1GB     minimum on-disk size

CLEAN-ONLY FLAGS
  --dry-run           print what would be removed, do not actually do it

EXAMPLES
  wtkill                                       # open TUI
  wtkill list --json                           # JSON dump for AI agents
  wtkill list --older-than 30d                 # text list of stale worktrees
  wtkill clean --prunable --force              # remove all prunable worktrees
  wtkill clean --older-than 60d --dry-run      # preview what 'older than 2mo' would nuke
  wtkill rm /path/to/worktree --force          # remove a specific path

TUI KEYS
  ↑↓ / jk     navigate           f       toggle --force
  space       delete selected    m       toggle include-main (rescan)
  r           rescan             q       quit

%s
%s
MIT licensed.
`, version, author, repoURL)
}
