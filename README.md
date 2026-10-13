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

## Generic CSV export

The initial exporter is deliberately vendor-neutral. It outputs stable headers for the library's normalized data, including receive/transmit frequencies, tones, tags, notes, and group coordinates.

```sh
./rflib export csv --output channels.csv --tag ham
./rflib export csv --group "Portland Metro" > metro.csv   # stdout when --output is omitted
```

This is not yet a CHIRP or Yaesu import file. Those formats will be separate tested export profiles so their field and tone mappings are explicit.

## Development

```sh
make check    # go vet, golangci-lint, and go test
```
