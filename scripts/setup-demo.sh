#!/usr/bin/env bash
# Creates a deterministic playground with several git worktrees so demos
# (vhs, asciinema, screenshots) are reproducible.
set -euo pipefail

ROOT="${1:-/tmp/wtkill-demo}"
rm -rf "$ROOT"
mkdir -p "$ROOT"

make_repo() {
  local name="$1"
  shift
  local repo="$ROOT/$name"
  mkdir -p "$repo"
  (
    cd "$repo"
    git init -q -b main
    git config user.name "demo"
    git config user.email "demo@example.com"
    git config commit.gpgsign false
    echo "# $name" > README.md
    git add . && git commit -q -m "init"

    for spec in "$@"; do
      branch="${spec%%:*}"
      ageDays="${spec#*:}"
      git branch "$branch" >/dev/null 2>&1 || true
      wt_path="$repo/.worktrees/$branch"
      git worktree add -q "$wt_path" "$branch"
      (
        cd "$wt_path"
        echo "work on $branch" > work.txt
        git add . && git commit -q -m "work on $branch"
        # backdate the commit so age shows correctly in the TUI
        if [ "$ageDays" -gt 0 ]; then
          past=$(date -v-"${ageDays}"d +%Y-%m-%dT%H:%M:%S 2>/dev/null \
                || date -d "$ageDays days ago" +%Y-%m-%dT%H:%M:%S)
          GIT_COMMITTER_DATE="$past" git commit -q --amend --no-edit --date "$past"
        fi
        # add some bulk so size column has something to show
        dd if=/dev/zero of=bulk.bin bs=1M count="${SIZE_MB:-3}" 2>/dev/null
      )
    done
  )
}

SIZE_MB=8 make_repo crimson-desert  "feat/landing-redesign:9"  "fix/anchor-position:27"
SIZE_MB=12 make_repo blossom-agents  "feat/DATA-1216:0"          "fix/edge-ports:5"
SIZE_MB=5 make_repo pingevent       "hotfix-docker-oom:62"     "worktree-landing-fix:65"
SIZE_MB=2 make_repo skills          "feat/auto-detect:32"

echo "demo ready at $ROOT"
