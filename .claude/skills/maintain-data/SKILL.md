---
name: maintain-data
description: Resolve rflib data-check findings - built-in channel lists, radio models, and FCC band-plan rules that differ from their sources. Use when asked to maintain data, work data-check issues, or review a data source.
---

# Maintain rflib's shipped data

The weekly `Data check` workflow (`.github/workflows/datacheck.yml`) runs `tools/datacheck`, which compares built-in lists and radio models with the sources in `tools/datacheck/sources.yaml`, and opens or updates one GitHub issue per dataset (label `data-check`). It needs no agent. This skill handles the findings that need judgment.

## 1. Gather the findings

Start from evidence, not from scratch:

```sh
gh issue list --label data-check --state open
uv run tools/chirp_model.py --json > /tmp/chirp.json   # CHIRP checks need this
go run ./tools/datacheck -chirp /tmp/chirp.json         # or -only ID,ID
```

The local run shows the current state; an issue may be stale if the data changed since. Work one dataset at a time.

## 2. Resolve each finding

Read the source before changing anything; never edit data from memory. Each finding ends one of three ways:

- **The data is wrong or out of date:** fix the list (`internal/seed/data/*.csv`), the model (`internal/model/builtin/*.yaml`), or the band table (`internal/domain/bandplan.go`).
- **The difference is accepted:** record it in `sources.yaml` - add to `ignore` (frequencies in MHz such as `"50.323"`, names, or `"<model>: <field>"` for CHIRP models) with a comment saying why.
- **Reviewed and still right:** move `verified` (review datasets) or `recorded` (eCFR sections) to the date or amendment you checked against.

By kind:

| Kind | What to do |
|---|---|
| `wsjtx`, `js8` | The app's default list changed. Add or drop the dial frequency, keeping no channel in two lists (TestListsDontOverlap): a shared dial stays in one list, noted in the other's description. |
| `amsat-fm` | A satellite appeared, changed, or left AMSAT's list. Check AMSAT's status page before adding or removing one; a satellite missing for one week may be a page edit. |
| `nws-radiofax` | A frequency changed, or a schedule is gone. If NWS ended a station's broadcasts (look for a Service Change Notice), remove its channels; if all are gone, remove the list and its dataset. |
| `ecfr` | A rule section was amended. Read the change in the eCFR (its history view compares versions), decide whether the band table, band-plan warnings, or a list must change, then set `recorded` to the amendment date. |
| `chirp-models` | A curated model differs from CHIRP's driver. Our models cite CHIRP's source, so usually the model follows CHIRP; when the model is right (the manual, or receive-only extras that rflib's band check doesn't need), ignore the field with a reason. |
| `chirp-radios` | New radios in CHIRP. Nothing to add unless someone asks for a curated model (users can use `chirp-generic` or `tools/chirp_model.py`). Update `chirp-radios.txt` from `uv run tools/chirp_model.py --list`. |
| review | Check the list against its `source` by hand, fix what changed, and move `verified`. |

A new built-in list or model must be added to a dataset's `covers` (TestEveryListAndModelIsCovered); cite where its data came from.

## 3. Verify and hand off

```sh
go run ./tools/datacheck -chirp /tmp/chirp.json -only <datasets you touched>
make check
```

Then propose a commit on a branch from `origin/main`, with `Fixes #N` for each issue resolved, and stop: the user commits, pushes, and merges. Summarize each finding's outcome (fixed, accepted, or reviewed) and the source you checked it against.
