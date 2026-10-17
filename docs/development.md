# Development

## Build

To build rflib from source instead:

```sh
make build    # or: go build -o rflib ./cmd/rflib
```

Building needs Go. The web interface also needs Node.js to build; without it, `rflib` works, and `rflib serve` serves a page saying the interface isn't built (see [Web interface](web-interface.md)).

## Checks and code layout

```sh
make check      # go vet, golangci-lint, and go test
make web        # build the web interface into web/dist (needs Node.js)
make build      # build rflib, embedding web/dist when it's built
make web-check  # type-check the interface and run its unit tests
make e2e        # rebuild everything (replacing ./rflib) and run the browser smoke tests
```

The web interface lives in `web/`: React, TypeScript, Vite, Mantine, and TanStack Query. Its API types are generated from `internal/api/openapi.yaml` (`npm run gen`, run by every build), so an API change that breaks the interface fails its type check. For development, run `rflib serve` and `npm run dev` in `web/`; Vite serves the interface and passes `/api` through.

The CLI (`cmd/rflib`) parses flags and prints results, and the HTTP API (`internal/api`) decodes requests and encodes responses. The operations themselves live in `internal/app`, which both share. Errors made by a request are `domain.ErrInvalid` (`domain.Invalidf`), which the API reports as 400; other failures are 500. `internal/app` works on one library through the `store.Store` and `store.Library` interfaces (`internal/store`); `internal/store/sqlite` implements them. Any other backend must pass the contract tests in `internal/store/storetest`.

## Releases

Versions are `v0.MINOR.PATCH` until 1.0. Pushing a tag makes a release: the `Release` workflow runs the tests, builds every archive with `make dist`, and publishes them, with their checksums and notes drawn from the pull requests merged since the last release.

```sh
git tag v0.1.0
git push origin v0.1.0
make dist VERSION=v0.1.0    # the same archives, locally, into dist/
```

The `CI` workflow checks every pull request: Go's checks on Linux, macOS, and Windows, the linter, the web interface's checks, and the browser tests.

## Maintaining built-in data

Built-in lists and radio models come from outside sources: WSJT-X's and JS8Call's frequency lists, AMSAT, the NWS radiofax schedules, the FCC rules in the eCFR, and CHIRP's drivers. `tools/datacheck/sources.yaml` records each one's source and how it's checked, and every built-in list and model must appear there. The `Data check` workflow runs weekly and opens or updates one issue per dataset that differs from its source (label `data-check`). To run it locally:

```sh
uv run tools/chirp_model.py --json > /tmp/chirp.json   # CHIRP checks need this
go run ./tools/datacheck -chirp /tmp/chirp.json         # -only ID,ID for some datasets
```

Resolve a finding by fixing the data, or by recording an accepted difference or a fresh review in `sources.yaml`. In Claude Code, `/maintain-data` works through open findings. GitHub stops scheduled workflows in repositories with no activity for 60 days; re-enable it from the Actions tab, or run the same commands from cron with `GH_TOKEN` set and `-issues`.

---

[Documentation index](README.md)
