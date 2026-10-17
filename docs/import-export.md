# Import and export

## Export

Exports write to stdout unless `--output PATH` is given, and select channels with `--query` (see [Search](search.md)).

```sh
./rflib export csv --output channels.csv --tag ham
./rflib export csv near:45.52,-122.68,40 > metro.csv
./rflib export json --output backup.json
```

- **`export csv`** is deliberately vendor-neutral: stable headers for the library's normalized channel data, including receive/transmit frequencies, tones, tags, notes, service, receive-only, short names, and location. New columns are only ever added at the end.
- **`export chirp`** writes a file for CHIRP's *File > Import*. See [CHIRP](#chirp) below.
- **`export profile NAME`** writes a radio programming tool's own CSV format, such as Yaesu ADMS. See [Export profiles](export-profiles.md).
- **`export json`** is a full-fidelity backup, including IDs and timestamps, which the CSV leaves out. Your notes, tags, and short names on shared channels are kept, with the shared channel as it was; restored into a database without that shared channel, it becomes a private copy. `rflib shared export json` backs up the shared list, with its organizations, systems, and plans, and `rflib shared import json` restores it. The CSV writes a shared channel's tags and notes and yours together.

## Import

```sh
./rflib import csv channels.csv --dry-run
./rflib import csv channels.csv --tags imported
./rflib import json backup.json
./rflib import chirp my-radio.csv --tags handheld
./rflib export csv --tag ham | ./rflib --db other.db import csv
```

`import csv` reads the generic CSV format. Columns are matched by header name, so they can be in any order and extra columns are ignored. Only `name` and `receive_mhz` are required, which makes hand-built spreadsheets easy to load. `import json` restores a backup exactly. Your notes, tags, and short names on a shared channel go back on it when it's in this database's shared list; otherwise the channel becomes a private copy, with a warning. Imported channels are always private and active. `import chirp` reads a file saved with CHIRP's *File > Export*.

- **All or nothing:** any invalid row is reported with its line number and nothing is imported. Use `--skip-invalid` to import the valid rows anyway. A failure partway through also writes nothing.
- **`--dry-run`** runs the whole import and then discards it, so its summary is exactly what a real import would do.
- **Duplicates:** a channel is a duplicate when it has the same ID as an existing channel, or the same name, receive and transmit frequencies, tones, and tone squelch. Channels that only share a name and frequency, such as two sites with different tones, are kept apart. `--on-duplicate` chooses `skip` (the default), `update`, or `create`. `--match programmed` drops the name from the comparison and matches on frequencies, tones, and tone squelch alone. Use it for a file read from a radio, where names are shortened. With `--on-duplicate update`, the matched channel takes the file's name. Two rows in the same file with the same programming also match each other. `update` changes details like power, notes, skip, and tags; a different tone or frequency counts as a new channel. Re-running an import is therefore safe.
- **`--tags`** adds tags to every imported channel.

## CHIRP

```sh
./rflib export chirp --tag ham --sort freq --max-name-len 8 --output ft60.csv
```

For your own radios, `radio export` is simpler: it knows the model, your order, power, and name length. `export chirp` exports a filtered set of channels without a radio.

Open the radio's image in CHIRP, then use *File > Import* on the exported file. Memories are numbered from `--start` (default 1) in `--sort` order: `name` (the default) or `freq`. The `name` order compares numbers in names by value, so "GMRS 2" comes before "GMRS 10", and "GMRS 15" before "GMRS 15R". Use `--max-name-len` to fit names to the radio's display (see [Names on radio displays](names.md)).

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
| Power | Watts of the radio's level when exporting a radio, otherwise blank (see [Power](power.md)) |
| Notes | Comment |

The receive tone is only programmed when tone squelch is on, since that is what makes the radio use it.

CHIRP export accepts every library mode, but only the 50 standard CTCSS tones and the 104 standard DCS codes. An export that includes anything else fails and names every such channel. Add `--skip-invalid` to leave them out with a warning instead.

`import chirp` does the reverse. Memory locations aren't kept, unnamed memories are named after their frequency, and memories in modes the library doesn't support yet (digital modes such as DV, DMR, and P25) are reported as invalid rows. Pass `--service` to label everything you import.

---

[Documentation index](README.md)
