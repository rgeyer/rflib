# Agent notes: rflib

How to work in this repository, for coding agents. Read the [README](README.md) and the [documentation index](docs/README.md) for what rflib does; this file is how the maintainer, Ryan Geyer (W6FAM), wants changes made.

## Working with the maintainer

- **Check where things stand first.** `git fetch`, then check `origin/main` and `gh pr list --state all` before starting work or saying whether something is merged. A branch that has merged is finished: start a new one.
- **Branch from `origin/main`** (`git checkout -b <type>/<name> --track origin/main`), named `feature/`, `fix/`, `docs/`, or `chore/`. Stack on an unmerged branch only when the work needs it, and say so.
- **The maintainer commits, pushes, and merges.** On a new branch, leave the work uncommitted and give the commit commands and a proposed message. Never push, tag, or open or merge a PR.
- **UAT feedback is the exception.** During user acceptance testing, the maintainer says per item whether to fix it now or defer it (to the roadmap or a GitHub issue); ask when they don't say. Fixes go on one UAT branch (`feature/uat-feedback-N`), which the agent commits to itself, one descriptive commit per fix, still never pushing.
- **Commit only what passes.** Run the checks below before every commit, and don't chain a commit onto a check that might fail.
- **Commit messages** follow Conventional Commits (`feat(scope): ...`, `fix:`, `docs:`, `chore:`), with a body saying what changed for the user and why, wrapped near 72 columns. Add the attribution trailer your harness specifies.
- **The roadmap lives outside the repo,** in the maintainer's plan file. Deferred items go there or into GitHub issues, as the maintainer says.
- **No time estimates,** and no client or company names in code, commits, or docs.

## Checks

| Command | What it runs | When |
|---|---|---|
| `make check` | `go vet`, golangci-lint, and `go test ./...` | Every Go change |
| `make web-check` | TypeScript, generated API types, and vitest | Every web or API spec change |
| `make e2e` | Builds the web interface and `./rflib`, then the Playwright tests against `rflib serve` | Every change a user would see in the web interface |

- `make e2e` runs the whole suite: later tests depend on data earlier ones create, so one test run alone with `-g` can fail for no reason of its own.
- `gofmt -l` reports `cmd/rflib/cli_test.go`, `internal/app/app_test.go`, and `internal/export/json.go` already; leave those alone, but keep new code formatted.
- The web interface's API types (`web/src/api/schema.d.ts`) are generated from `internal/api/openapi.yaml` (`npm run gen`, which typecheck runs) and aren't committed.

## Layout

- `cmd/rflib`: the CLI (cobra), one file per command group, with `cli_test.go` driving it end to end.
- `internal/app`: rflib's operations; `internal/store` (SQLite, migrations in `store/sqlite/db.go`); `internal/domain` (channels, tones, band plans, power, shared tags); `internal/query` (the search language); `internal/program` (laying out and exporting a radio); `internal/model` and `internal/profile` (radio models and export formats, built-in YAML plus the user's own files); `internal/seed` (built-in channel lists); `internal/api` (the HTTP API; `openapi.yaml` is its contract, and a test checks every route is in it).
- `web/`: the React and Mantine web interface, with Playwright tests in `web/e2e` and the demo in `web/demo`.
- `tools/`: `datacheck` (built-in data against its sources), `dist` (release archives), `chirp_model.py` (drafts a model from a CHIRP driver).
- `docs/`: the documentation, one page per topic; `site/`: the GitHub Pages landing page.

## Writing

- **The README stays lean:** intent, install, the quick start, where to ask, and the license. Document a feature in its `docs/` page (or a new page, added to `docs/README.md`), never by growing the README. Each page ends with a link back to the index.
- **Update `docs/limitations.md`** when a limitation is fixed or found.
- **Plain, direct prose, with the Oxford comma,** in docs, help text, comments, and UI text. Say what something does for the user.
- **Example callsigns are impossible ones:** `N0CALL`, `N1CALL`, `W1CALL`, `VE3CALL`, `2E0CALL`, and the like. Never a real operator's.
- **The source link stays.** rflib is AGPL-3.0: the web interface links to the running build's source (`buildinfo.SourceURL`, or `serve --source-url`), and that link must remain on the sign-in page and in the sidebar.

## Data

- **Never open the maintainer's own rflib databases for writing.** Read them read-only (such as Python's `sqlite3` with `mode=ro`), or work on a copy.
- **Built-in channel lists** are CSVs in `internal/seed/data`, each described in `internal/seed/seed.go` and covered in `tools/datacheck/sources.yaml`. Channels are matched across versions by name, so renaming one means recording its old name in the list's `Renamed`, or an update retires it and adds another. Tests count each list's channels.
- **Shared tags** are `domain.CuratedTags`; a list may carry only those, and never a tag that repeats a channel's fields (service, band, mode, duplex).
- **A new radio model** is a YAML file in `internal/model/builtin`, added to `TestBuiltinsAreValid`'s list, covered in `sources.yaml`, listed in `docs/radio-models.md`'s table, and counted in `docs/limitations.md`. Base it on evidence: CHIRP's driver (`tools/chirp_model.py`), or exports from the radio's own software.
- **Schema changes** are new numbered migrations in `internal/store/sqlite/db.go`; `rflib init` applies them.

## Releases and media

- Versions are `v0.MINOR.PATCH` until the maintainer names 1.0. Pushing a tag runs the release workflow, which builds every system's archive (`make dist`) and `SHA256SUMS`; binaries are unsigned.
- The README's recording and the docs' screenshots come from `web/demo` (see its README); re-render them when the screens they show change.
- The hosted instance's deployment lives in another repository, not here.
