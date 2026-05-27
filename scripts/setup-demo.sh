#!/usr/bin/env bash
# Reproducible playground for recording wtkill demos.
# Creates ~15 worktrees across realistic-looking repos with varied sizes and
# ages so the GIF/asciicast looks like a real cleanup session.
set -euo pipefail

ROOT="${1:-/tmp/wtkill-demo}"
rm -rf "$ROOT"
mkdir -p "$ROOT"

# make_repo <category/name> <bulk_mb> <branch:ageDays:bulkMB[:dirty]> ...
make_repo() {
  local name="$1"; shift
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
      IFS=':' read -r branch ageDays bulkMB dirty <<< "$spec"
      git branch "$branch" >/dev/null 2>&1 || true
      local wt_path="$repo/.worktrees/$(basename "$branch")"
      git worktree add -q "$wt_path" "$branch"
      (
        cd "$wt_path"
        # bulk file so `du -sk` shows a believable size
        dd if=/dev/urandom of=artifact.bin bs=1m count="$bulkMB" 2>/dev/null
        git add . && git commit -q -m "wip on $branch"
        if [ "$ageDays" -gt 0 ]; then
          local past
          past=$(date -v-"${ageDays}"d +%Y-%m-%dT%H:%M:%S 2>/dev/null \
                || date -d "$ageDays days ago" +%Y-%m-%dT%H:%M:%S)
          GIT_COMMITTER_DATE="$past" GIT_AUTHOR_DATE="$past" \
            git commit -q --amend --no-edit --date "$past"
        fi
        # leave the worktree "dirty" → git worktree remove will refuse without --force
        if [ "${dirty:-0}" = "dirty" ]; then
          echo "uncommitted change" >> artifact.bin
        fi
      )
    done
  )
}

# repos with branches: branch:ageDays:bulkMB[:dirty]
make_repo crimson-desert \
  "claude/epic-neumann:9:380" \
  "feat/guides-patch:24:260" \
  "fix/anchor-position:27:120"

make_repo blossom-agents \
  "feat/DATA-1099-mesh:20:180" \
  "feat/DATA-1216:0:60" \
  "fix/DATA-1099-edge-ports:5:140:dirty"

make_repo BlossomOLBAdminFront \
  "feat/DATA-1090:21:220" \
  "feat/DATA-1031:32:160" \
  "feat/attachments-ux:40:55"

make_repo pingevent \
  "hotfix-docker-oom:62:90" \
  "feat/landing-animations:65:75"

make_repo blossom-pay-backend \
  "worktree-agent-afe017f8:30:8" \
  "worktree-agent-a96bfeb7:30:8" \
  "bug/CHAT-MAIN01:30:55"

make_repo blossom-skills \
  "feat/auto-detect:32:22" \
  "feat/parent-folder-repos:28:22"

echo
echo "demo ready at $ROOT"
du -sh "$ROOT" 2>/dev/null || true
