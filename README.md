# wtkill

> Find and clean stale `git worktree` entries across all your repos — interactive TUI for humans, JSON output for AI agents and scripts.

![wtkill in action — scan, delete, force-retry](./docs/demo.gif)

`wtkill` is the [`npkill`](https://npkill.js.org/) of git worktrees. Point it at a directory full of repos, it shows you every worktree across all of them with size and age, and you delete the stale ones with one keystroke.

It also exposes a clean non-interactive CLI with JSON output so AI agents (Claude Code, Codex, Cursor, etc.) and shell scripts can use it without a terminal.

## Why

Worktrees are great until you forget them. Six months in you have 60 GB of branches you'll never touch again, scattered across `.worktrees/`, `.claude/worktrees/`, and one-offs you made on a Tuesday. `git worktree list` only shows them per-repo, and `du` on `~/Projects` lies to you because it counts symlinks weirdly.

`wtkill` walks your projects directory, runs `git worktree list --porcelain` on every repo it finds, enriches each entry with `du -sk` and `git log -1`, and renders the whole picture sorted by what's eating the most disk.

## Features

- **Recursive scan** of any directory looking for git repos (default `~/Projects`, configurable)
- **Live concurrent scan** — 8 worktrees enriched in parallel via goroutines, results stream into the UI as they arrive
- **Symlink-aware dedupe** so repos that share a `.git` via symlink only show their worktrees once
- **TUI** with selection, sorted-by-size, status indicators (`prunable`, `missing`, `deleted`, `failed`), age-coloured (green ≤7d, yellow ≤30d, red >30d)
- **Non-interactive CLI** with subcommands `list`, `rm`, `clean` and JSON output by default when stdout is piped
- **Filters** for `list` and `clean`: `--older-than`, `--branch <regex>`, `--prunable`, `--missing`, `--min-size`
- **Safety:** `clean` without filters refuses to run; `--dry-run` previews; `--force` is opt-in
- **Single static binary**, ~4.6 MB, no runtime dependencies

## Install

### From source

Requires Go 1.25+.

```bash
git clone https://github.com/ohernandezdev/wtkill
cd wtkill
go build -o wtkill .
mv wtkill ~/.local/bin/   # or anywhere on $PATH
```

### Homebrew (macOS Apple Silicon / Linux)

```bash
brew install ohernandezdev/tap/wtkill
```

### Prebuilt binaries

Tarballs for `darwin-arm64`, `linux-amd64`, `linux-arm64` are on the [releases page](https://github.com/ohernandezdev/wtkill/releases/latest).

```bash
# example: macOS Apple Silicon
curl -L https://github.com/ohernandezdev/wtkill/releases/latest/download/wtkill-v0.3.0-darwin-arm64.tar.gz | tar xz
mv wtkill ~/.local/bin/   # or anywhere on PATH
```

## Usage

### Interactive TUI

```bash
wtkill                       # scan ~/Projects
wtkill --path ~/code         # scan a different root
wtkill --depth 6             # walk deeper than the default 4
wtkill --include-main        # also list each repo's main worktree
```

**Keys**

| key | action |
| --- | --- |
| `↑↓` / `j` `k` | navigate |
| `PgUp` / `PgDn` | jump a page |
| `g` / `G` | first / last |
| `space` | delete selected worktree |
| `f` | toggle `--force` (for worktrees with uncommitted changes) |
| `m` | toggle including each repo's main worktree (rescans) |
| `r` | rescan |
| `q` / `Ctrl+C` | quit |

When a delete fails, the row turns `✗ failed` and the footer shows git's error message for that row. Press `f` to enable `--force`, then `space` again to retry.

### CLI (for AI agents and scripts)

When `stdout` is not a TTY, `wtkill list` and `wtkill rm` automatically emit JSON.

```bash
# JSON list of every worktree
wtkill list --json

# Filtered text list
wtkill list --older-than 30d --min-size 100MB

# Bulk-clean: preview what 60-day-old branches would be removed
wtkill clean --older-than 60d --dry-run

# Bulk-clean: remove every worktree git considers prunable
wtkill clean --prunable --force

# Remove specific paths
wtkill rm /path/to/worktree-a /path/to/worktree-b --force
```

#### JSON schema for `list`

```jsonc
{
  "root": "/Users/me/Projects",
  "scanned_repos": 122,
  "elapsed_ms": 15386,
  "total_bytes": 19789389824,
  "count": 8,
  "worktrees": [
    {
      "repo": "/Users/me/Projects/my-app",
      "path": "/Users/me/Projects/my-app/.worktrees/feature-x",
      "branch": "feature-x",
      "head": "e5d70bfe944da1ab956ad1177876767fc99ebede",
      "detached": false,
      "bare": false,
      "prunable": false,
      "missing": false,
      "size_bytes": 4519583744,
      "age_days": 9
    }
  ]
}
```

#### JSON schema for `rm` / `clean`

```jsonc
{
  "results": [
    {
      "path": "/Users/me/Projects/foo/.worktrees/bar",
      "repo": "/Users/me/Projects/foo",
      "ok": true
    }
  ],
  "failed": 0
}
```

`clean` adds `dry_run`, `matched`, `removed`, `failed`, `freed_bytes` at the top level.

### Filters reference

Applies to `list` and `clean`.

| flag | example | meaning |
| --- | --- | --- |
| `--older-than <dur>` | `--older-than 30d` | last commit older than this (`s` `m` `h` `d` `w`) |
| `--branch <regex>` | `--branch '^claude/'` | branch name matches regex |
| `--prunable` | | only worktrees git already considers prunable |
| `--missing` | | only worktrees whose path no longer exists on disk |
| `--min-size <size>` | `--min-size 500MB` | minimum on-disk size (`B` `KB` `MB` `GB` `TB`) |

### Exit codes

| code | meaning |
| --- | --- |
| 0 | success |
| 1 | one or more operations failed (partial success) |
| 2 | usage error (bad flag, missing required filter, etc.) |

## AI agent integration

Drop this into your agent's system prompt or tool definition:

> You have access to the `wtkill` CLI. Use `wtkill list --json` to discover git worktrees under the user's projects directory, then `wtkill rm <path> --force --json` to remove specific ones, or `wtkill clean <filters> --dry-run --json` to preview a bulk operation. Always do a `--dry-run` before any `clean` and surface what it would remove to the user.

Because output is JSON by default off-TTY and exit codes are well-defined, agents can parse results without regex on stderr output.

## How it works

1. **Discovery** — `wtkill` walks the configured root with `os.ReadDir`, depth-limited, skipping common heavy dirs (`node_modules`, `target`, `.next`, etc.). A directory is a repo if it contains a `.git` entry.
2. **Dedupe** — for each candidate repo it runs `git rev-parse --absolute-git-dir`, resolves symlinks via `filepath.EvalSymlinks`, and dedupes so two paths sharing the same `.git` only get scanned once.
3. **Enrichment** — for each repo it calls `git worktree list --porcelain` to get the worktree list, then for each non-main worktree it runs `du -sk` for size and `git log -1 --format=%ct` for age. These are dispatched as goroutines bounded by a semaphore (8 concurrent).
4. **Render** — the TUI is [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Elm-Architecture model/update/view) styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss). Updates stream in from a channel as worktrees are enriched; the list re-sorts by size on every insert.
5. **Delete** — `git worktree remove <path>` (with `--force` if toggled), followed by `git worktree prune` to clean stale admin entries.

## What wtkill is not

- It does not delete branches, tags, or remotes. Only worktree entries.
- It does not touch your main worktree unless you pass `--include-main`.
- It is not a replacement for [`npkill`](https://npkill.js.org/) — npkill is for `node_modules`; this is for git worktrees. Different scope.
- It does not currently know how to GC orphan `.git/worktrees/<name>/` admin directories that git left behind after a manual `rm -rf`. Use `git worktree prune` for that.

## Contributing

PRs welcome. The codebase is small and intentionally so:

- `main.go` — entrypoint, subcommand dispatch, version, help
- `scan.go` — concurrent repo discovery, worktree enrichment, symlink dedupe, `git worktree remove`
- `model.go` — Bubble Tea model + Lip Gloss render
- `cmd.go` — non-interactive `list` / `rm` / `clean` and filter parsing

Before opening a PR:

```bash
go fmt ./...
go vet ./...
go build -o wtkill .
./wtkill list --json --depth 2 | head -20   # smoke test
```

## License

[MIT](./LICENSE) © [Omar Hernández (@ohernandezdev)](https://github.com/ohernandezdev)

Built on top of [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) by [@charmbracelet](https://github.com/charmbracelet). Inspired by [npkill](https://npkill.js.org/).
