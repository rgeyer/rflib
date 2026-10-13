# rflib

`rflib` is a local, SQLite-backed library for analog radio channels. It stores source information separately from radio-programming formats, so future exporters can adapt the same library to CHIRP and vendor-specific software.

## Build

```sh
make build    # or: go build -o rflib ./cmd/rflib
```

## Library location

Every command takes `--db PATH`. When it is omitted, `rflib` uses `$RFLIB_DB`, then `$XDG_DATA_HOME/rflib/library.db` (`~/.local/share/rflib/library.db` by default). The examples below set `RFLIB_DB` once instead of repeating `--db`.

The CLI only creates a database when you explicitly initialize one. After upgrading `rflib`, commands may report that the library schema is out of date; re-run `init` to upgrade it in place.

```sh
export RFLIB_DB=./radio-library.db
./rflib init
```

Back up the database by copying it while the CLI is not running.

## Groups

Groups are optional geographic collections. Provide latitude and longitude together; radius is optional metadata for the group itself.

```sh
./rflib group add --name "Portland Metro" \
  --lat 45.5152 --lon -122.6784 --radius-km 40 --description "Local operating area"
./rflib group list
```

Groups can be referenced by ID or by their unique name anywhere a group is expected. Deleting a group keeps its channels and leaves them ungrouped.

## Channels

Channels record a receive frequency plus an optional transmit frequency. Use matching RX/TX values for simplex, different values for a repeater, and omit `--tx` for receive-only records.

```sh
./rflib channel add --name "National Calling" \
  --rx 146.520 --tx 146.520 --mode FM --tags "ham,simplex"

./rflib channel add --name "Local Repeater" \
  --group "Portland Metro" --rx 146.940 --tx 146.340 --mode FM \
  --rx-tone ctcss:100.0 --tx-tone ctcss:100.0 --tone-squelch --tags "ham,repeater"

./rflib channel list --tag repeater
./rflib channel list --lat 45.52 --lon -122.68 --radius-km 30
```

Supported modes are `FM`, `NFM`, `AM`, `USB`, and `LSB`. Channel frequencies are entered in MHz and stored exactly as integer Hz. CTCSS tones are entered in Hz and stored exactly as integer millihertz, preserving standard values such as `69.3 Hz` without floating-point math or rounding.

Tone syntax is normalized and exporter-neutral:

- **No tone:** `none`
- **CTCSS:** `ctcss:100.000`
- **DCS normal:** `dcs:023`
- **DCS inverted:** `dcs:023:inverted`

Updates change only explicitly supplied flags. Supplying an empty `--tx`, `--group`, or `--tags` clears that field, and boolean flags can be turned off explicitly (`--skip=false`).

```sh
./rflib channel update --id <channel-id> --power 25W
./rflib channel delete --id <channel-id> --yes
```

`list` and `show` commands accept `--format json` for scripting.

`--short-names` gives a channel your own abbreviations for radios with narrow displays, such as `--short-names "SB Sheriff Disp,SBSO Disp"`. See [Names on radio displays](#names-on-radio-displays).

## Services and receive-only channels

Each channel can record its radio service with `--service`: `amateur`, `gmrs`, `frs`, `cb`, `murs`, `marine`, `noaa`, `public_safety`, or `other`. Leaving it out means unspecified; `rflib` never guesses from frequency, since services such as FRS and GMRS share channels. `channel list`, `export`, and `check` accept `--service` as a filter.

`--rx-only` marks a channel you must never transmit on. Unlike omitting `--tx`, it keeps the system's input frequency on record (useful for public safety repeaters), while exports such as CHIRP program the channel receive only. `noaa` and `public_safety` channels are receive only unless you pass `--rx-only=false`.

```sh
./rflib channel add --name "SO Dispatch" --rx 460.275 --tx 465.275 --service public_safety
./rflib import chirp radioreference.csv --skip-invalid --service public_safety
```

To label channels you already imported, re-import the same file with `--on-duplicate update --service ...`. Service and receive-only don't count when matching duplicates, so the existing channels are updated in place.

### Band plan checks

Saving or importing a channel warns, without failing, when it doesn't fit its service's US (FCC) channel plan. That covers amateur transmit frequencies outside the amateur bands, off-plan FRS, GMRS, MURS, CB, and NOAA frequencies, repeater splits on simplex-only services, and transmitting NOAA or public safety channels. `rflib check` reports every such channel in the library and exits non-zero when it finds any.

## Built-in channel lists

```sh
./rflib seed list
./rflib seed show us-gmrs
./rflib seed install us-gmrs us-noaa --tags handheld
```

| List | Channels |
|---|---|
| `us-frs` | FRS 1-22, with power limits in the notes |
| `us-gmrs` | GMRS 1-22 plus repeater pairs 15R-22R (add your repeater's tone) |
| `us-murs` | MURS 1-5 |
| `us-cb` | CB 1-40, AM |
| `us-noaa` | NOAA Weather Radio WX1-WX7, receive only |
| `us-ham-calling` | Amateur FM and SSB simplex calling frequencies, 10 m through 70 cm |

Installing a list again skips the channels already added. `install` takes the same `--group`, `--tags`, `--on-duplicate`, and `--dry-run` flags as `import`.

## Export

Exports write to stdout unless `--output PATH` is given, and accept the same filters as `channel list`.

```sh
./rflib export csv --output channels.csv --tag ham
./rflib export csv --group "Portland Metro" > metro.csv
./rflib export json --output backup.json
```

- **`export csv`** is deliberately vendor-neutral: stable headers for the library's normalized channel data, including receive/transmit frequencies, tones, tags, notes, service, receive-only, short names, and group coordinates. New columns are only ever added at the end.
- **`export chirp`** writes a file for CHIRP's *File > Import*. See [CHIRP](#chirp) below.
- **`export profile NAME`** writes a radio programming tool's own CSV format, such as Yaesu ADMS. See [Export profiles](#export-profiles) below.
- **`export json`** is a full-fidelity backup, including IDs, timestamps, and group details that the CSV leaves out. Without filters it includes every group, even empty ones. With filters it includes only the groups the exported channels use.

## Import

```sh
./rflib import csv channels.csv --dry-run
./rflib import csv channels.csv --tags imported
./rflib import json backup.json
./rflib import chirp my-radio.csv --tags handheld
./rflib export csv --tag ham | ./rflib --db other.db import csv
```

`import csv` reads the generic CSV format. Columns are matched by header name, so they can be in any order and extra columns are ignored. Only `name` and `receive_mhz` are required, which makes hand-built spreadsheets easy to load. `import json` restores a backup exactly. `import chirp` reads a file saved with CHIRP's *File > Export*.

- **All or nothing:** any invalid row is reported with its line number and nothing is imported. Use `--skip-invalid` to import the valid rows anyway. A failure partway through also writes nothing.
- **`--dry-run`** runs the whole import and then discards it, so its summary is exactly what a real import would do.
- **Duplicates:** a channel is a duplicate when it has the same ID as an existing channel, or the same name, receive and transmit frequencies, tones, tone squelch, and group. Channels that only share a name and frequency, such as two sites with different tones, are kept apart. `--on-duplicate` chooses `skip` (the default), `update`, or `create`. `update` changes details like power, notes, skip, and tags; a different tone or frequency counts as a new channel. Re-running an import is therefore safe.
- **Groups:** channels are attached to groups by ID, then by name. Groups that don't exist yet are created and listed in the summary. `--group NAME` puts every imported channel in an existing group instead.
- **`--tags`** adds tags to every imported channel.

## Names on radio displays

Radio displays fit anywhere from 6 to 16 or more characters. Every export that numbers memories (`export chirp` and `export profile`) fits each name to the radio:

1. The full name, if it fits.
2. Otherwise the longest of the channel's `--short-names` that fits.
3. Otherwise a shortened name. Shortening starts from the shortest short name, which already carries your own abbreviations, or from the full name when there are none.

Shortening works through these steps, stopping as soon as the name fits:

1. **Abbreviate** with a built-in radio dictionary (Repeater → Rpt, Dispatch → Disp, Search and Rescue → SAR, Common Traffic Advisory Frequency → CTAF, and about 100 more), biggest saving first.
2. **Drop filler words** such as "of" and "the" (only in lower case, so the "A" in "Diablo A" stays).
3. **Remove spaces**, capitalizing each word so it stays readable ("MetroRpt").
4. **Trim common words.** When fitting a whole export, words that many channels share, such as an agency prefix, give way before the words that tell channels apart.
5. **Remove inner vowels** ("Lompoc" → "Lmpc").
6. **Trim words toward their initial**, leftmost first, so the last word (usually the channel's function) survives.
7. **Cut** what's left.

Some words get special treatment:
- **Identifiers survive.** Any word with a digit (`15R`, `1D`, `TAC 7`, a callsign) is kept until the final cut, so "SBCoSO Ops 1D" and "SBCoSO Ops 2D" stay distinct even at 6 characters.
- **Acronyms keep their vowels.** In mixed-case names, mostly-capital words such as "ARES" are left alone until the cut.
- **Punctuation and run-together words split.** Brackets and slashes count as word breaks, and run-together words split at capitals: "SantaBarbaraPD1" is read as "Santa Barbara PD1".

The same name always shortens the same way, so names stay stable between exports.

Preview names before exporting, and add a short name to any channel whose result you don't like:

```sh
./rflib names --max-len 6 --tag ham
./rflib names --profile yaesu-ftm6000 --dedupe freq
```

The method column shows how each name was produced: `fits`, `variant` (one of your short names), `shortened`, or `cut`.

**Duplicates.** When two channels end up with the same name on the radio, exports warn and leave them as they are. `--dedupe` tells them apart instead:
- `index` appends 1, 2, 3, and so on.
- `freq` appends the shortest trailing frequency digits that differ, for example "RitzOps65" and "RitzOps75". Channels sharing a frequency are numbered instead.

`--dedupe` works on `names`, `export chirp`, and `export profile`.

## Export profiles

Some radios are programmed with the manufacturer's software rather than CHIRP. An export profile describes such a tool's CSV memory format, and the same profile reads that format back in, so the files you already have become a starting library.

```sh
./rflib profile list
./rflib import profile yaesu-ftm150 "FTM-150 Software Programming.csv"
./rflib export profile yaesu-ftm150 --tag mobile --sort freq --output ftm150.csv
```

### Yaesu FTM-150 and FTM-6000

| Profile | Software | Names |
|---|---|---|
| `yaesu-ftm150` | ADMS-17 | 16 characters |
| `yaesu-ftm6000` | ADMS-13 | 6 characters |

In ADMS, open the *Memories* tab and use *File > Import* on the exported file. As ADMS's own exports do, the file holds all 999 memories: your channels first, then empty ones.

- **Power:** `HIGH`, `MID`, and `LOW` pass through. Any other label, such as `50W` from a CHIRP or RadioReference import, becomes `HIGH`.
- **Scan:** skipped channels are written with scan `NO`.
- **Narrow:** NFM channels are written as FM with *Narrow* on.
- **Repeaters:** written as `+RPT`/`-RPT` with the offset, and cross-band as `-/+`. The transmit frequency is always written as well.
- **Receive only:** these radios have no per-memory transmit inhibit, so receive-only channels are written as simplex. Both radios only transmit on 2 m and 70 cm.
- **Step:** the coarsest of 25, 5, 6.25, or 12.5 kHz that the frequency fits.
- **Names:** fitted to 16 (FTM-150) or 6 (FTM-6000) characters as described in [Names on radio displays](#names-on-radio-displays).
- **Tone modes:** only `OFF`, `TONE` (encode), and `DCS` are mapped, because those are the only spellings confirmed in ADMS exports. Tone squelch and split-tone channels fail the export (or are left out with `--skip-invalid`) until their ADMS spelling is confirmed. Override the profile, as below, to add them sooner.

Importing reads every non-empty memory. Unnamed memories are named after their frequency, and unused tone columns are ignored.

### Writing your own profile

Profiles are YAML. Start from a built-in:

```sh
./rflib profile show yaesu-ftm150 > my-radio.yaml
./rflib profile validate my-radio.yaml
./rflib export profile my-radio.yaml
```

To use a profile by name, put it in the directory `rflib profile list` prints (`~/.config/rflib/profiles` on Linux). A profile there with a built-in's name replaces the built-in, which is how to fix a format locally.

Each column names a `source` (a library value) and how to write it:

```yaml
name: my-radio
header: true            # write column names as the first row
line_ending: crlf       # or lf (default)
start: 0                # first memory number (default 1)
slots: 0                # pad to this many memories (0: channels only)
max_name_length: 8
name_case: upper
name_charset: "A-Z0-9 -/"   # ascii (default), alnum, or a literal set with ranges
standard_tones_only: true
columns:
  - {name: Ch, source: location}
  - {name: RX, source: rx_mhz, decimals: 4}
  - name: Squelch
    source: tone_mode
    map: {none: "", tone: T, tsql: T-SQL, dcs: D, tone_dcs: "T>D"}
  - {name: Tone, source: ctcss_hz, decimals: 1, suffix: " Hz", default: 88.5 Hz}
  - {name: Pwr, source: power, map: {HIGH: H, LOW: L, "*": H}}
  - {name: Fixed, source: const, value: "0"}
```

- **`map`** translates library values to the tool's text. `"*"` catches values that aren't listed. With a map, an unlisted value fails the export instead of passing through.
- **`default`** fills a column whose source is empty.
- **`empty`** fills padding rows.

| Source | Value |
|---|---|
| `location` | Memory number |
| `name`, `notes`, `service`, `tags`, `group` | Channel fields (notes on one line, tags joined with `;`) |
| `rx_mhz`, `tx_mhz`, `offset_mhz` | Frequencies in MHz. `tx_mhz` is the receive frequency for receive-only channels. |
| `duplex` | `none`, `plus`, `minus`, `split`, or `off` |
| `mode`, `narrow` | `FM`/`NFM`/`AM`/`USB`/`LSB`; `true` for NFM |
| `tone_mode` | `none`, `tone`, `tsql`, `dcs`, `dcs_enc`, `tone_dcs`, `dcs_tone`, `tone_tone`, `dcs_dcs`, `rx_tone`, or `rx_dcs` |
| `ctcss_hz`, `tx_ctcss_hz`, `rx_ctcss_hz` | CTCSS tones in Hz |
| `dcs_code`, `tx_dcs_code`, `rx_dcs_code`, `dcs_polarity` | DCS codes, and polarity as `NN`/`NR`/`RN`/`RR`. Without a polarity column, inverted DCS can't be exported. |
| `power`, `skip` | Power label (upper-cased); `true` when skipped |
| `step_khz` | First of the column's `steps_khz` that the frequency fits |
| `const` | The column's `value` |

## CHIRP

```sh
./rflib export chirp --tag ham --sort freq --max-name-len 8 --output ft60.csv
```

Open the radio's image in CHIRP, then use *File > Import* on the exported file. Memories are numbered from `--start` (default 1) in `--sort` order: `name` (the default), `freq`, or `group`. Use `--max-name-len` to fit names to the radio's display (see [Names on radio displays](#names-on-radio-displays)).

How library channels map to CHIRP:

| Library | CHIRP |
|---|---|
| TX equals RX | Duplex blank |
| TX within 50 MHz of RX | Duplex `+` or `-` with the offset |
| TX 50 MHz or more from RX (cross-band) | Duplex `split`, Offset is the TX frequency |
| No TX, or `--rx-only` | Duplex `off` |
| TX tone only | Tone `Tone` |
| Same CTCSS both ways, tone squelch on | Tone `TSQL` |
| Same DCS code both ways, tone squelch on | Tone `DTCS`, with polarity |
| Any other pair | Tone `Cross` with the matching cross mode |
| Skip | Skip `S` |
| Notes | Comment |

The receive tone is only programmed when tone squelch is on, since that is what makes the radio use it.

CHIRP export accepts every library mode, but only the 50 standard CTCSS tones and the 104 standard DCS codes. An export that includes anything else fails and names every such channel. Add `--skip-invalid` to leave them out with a warning instead.

`import chirp` does the reverse. Memory locations aren't kept, unnamed memories are named after their frequency, and memories in modes the library doesn't support yet (digital modes such as DV, DMR, and P25) are reported as invalid rows. Pass `--service` to label everything you import.

## Development

```sh
make check    # go vet, golangci-lint, and go test
```
