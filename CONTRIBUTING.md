# Contributing

Thanks for considering a contribution.

## Setup

```bash
git clone https://github.com/ohernandezdev/wtkill
cd wtkill
go mod download
go build -o wtkill .
```

Requires **Go 1.25+**.

## Smoke test before opening a PR

```bash
go fmt ./...
go vet ./...
go build -o wtkill .

# Should print structured JSON, exit 0
./wtkill list --json --depth 2 | head -20

# Should print usage error and exit 2
./wtkill clean ; echo $?

# Should preview without deleting, exit 0
./wtkill clean --older-than 365d --dry-run --depth 2 | tail -5
```

## What we want

- Performance work — `du -sk` is the main cost; faster size calculation is welcome
- Filters that match common cleanup heuristics (e.g. "branch merged into main")
- Output formats useful to other tools (CSV, TSV, NDJSON)
- Cross-platform fixes (Linux/Windows)
- Documentation, examples, asciicasts

## What we don't want

- Hidden destructive defaults. `clean` requires explicit filters for a reason.
- Auto-`--force` on failure. The user should choose.
- Network calls. This tool is local-only.
- Heavy dependencies. The current dependency tree is the budget.

## Style

- `go fmt` is law
- Errors propagate up; only `main` calls `os.Exit`
- TUI code keeps the Elm-Architecture split — pure `Update`, pure `View`
- Public-facing strings are English

## License

By contributing you agree your work is licensed under the [MIT License](./LICENSE).
