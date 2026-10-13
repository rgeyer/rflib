# rflib

`rflib` is a local, SQLite-backed library for analog radio channels. It stores source information separately from radio-programming formats, so future exporters can adapt the same library to CHIRP and vendor-specific software.

## Build

```sh
make build    # or: go build -o rflib ./cmd/rflib
```

## Library location

Every command takes `--db PATH`. When it is omitted, `rflib` uses `$RFLIB_DB`, then `$XDG_DATA_HOME/rflib/library.db` (`~/.local/share/rflib/library.db` by default). The examples below set `RFLIB_DB` once instead of repeating `--db`.

The CLI only creates a database when you explicitly initialize one. Re-running `init` upgrades an existing library to the current schema.

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

Supported modes are `FM`, `NFM`, and `AM`. Channel frequencies are entered in MHz and stored exactly as integer Hz. CTCSS tones are entered in Hz and stored exactly as integer millihertz, preserving standard values such as `69.3 Hz` without floating-point math or rounding.

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

## Export

Exports write to stdout unless `--output PATH` is given, and accept the same filters as `channel list`.

```sh
./rflib export csv --output channels.csv --tag ham
./rflib export csv --group "Portland Metro" > metro.csv
./rflib export json --output backup.json
```

- **`export csv`** is deliberately vendor-neutral: stable headers for the library's normalized channel data, including receive/transmit frequencies, tones, tags, notes, and group coordinates.
- **`export chirp`** writes a file for CHIRP's *File > Import*. See [CHIRP](#chirp) below.
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

## CHIRP

```sh
./rflib export chirp --tag ham --sort freq --max-name-len 8 --output ft60.csv
```

Open the radio's image in CHIRP, then use *File > Import* on the exported file. Memories are numbered from `--start` (default 1) in `--sort` order: `name` (the default), `freq`, or `group`. Use `--max-name-len` to fit the radio's display.

How library channels map to CHIRP:

| Library | CHIRP |
|---|---|
| TX equals RX | Duplex blank |
| TX within 50 MHz of RX | Duplex `+` or `-` with the offset |
| TX 50 MHz or more from RX (cross-band) | Duplex `split`, Offset is the TX frequency |
| No TX (receive only) | Duplex `off` |
| TX tone only | Tone `Tone` |
| Same CTCSS both ways, tone squelch on | Tone `TSQL` |
| Same DCS code both ways, tone squelch on | Tone `DTCS`, with polarity |
| Any other pair | Tone `Cross` with the matching cross mode |
| Skip | Skip `S` |
| Notes | Comment |

The receive tone is only programmed when tone squelch is on, since that is what makes the radio use it.

CHIRP only accepts FM, NFM, and AM, the 50 standard CTCSS tones, and the 104 standard DCS codes. An export that includes anything else fails and names every such channel. Add `--skip-invalid` to leave them out with a warning instead.

`import chirp` does the reverse. Memory locations aren't kept, unnamed memories are named after their frequency, and memories in modes the library doesn't support yet (such as DV or DMR) are reported as invalid rows.

## Development

```sh
make check    # go vet, golangci-lint, and go test
```
