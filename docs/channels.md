# Channels

Channels record a receive frequency plus an optional transmit frequency. Use matching RX/TX values for simplex, different values for a repeater, and omit `--tx` for receive-only records.

```sh
./rflib channel add --name "National Calling" \
  --rx 146.520 --tx 146.520 --mode FM --service amateur --tags "calling,mobile"

./rflib channel add --name "Local Repeater" \
  --rx 146.940 --tx 146.340 --mode FM --lat 45.5152 --lon -122.6784 --radius-km 40 \
  --rx-tone ctcss:100.0 --tx-tone ctcss:100.0 --tone-squelch --tags "ham,repeater,portland"

./rflib channel list tag:repeater
./rflib channel list 'service:none freq:144..148 is:repeater'
```

See [Search](search.md) for the query language.

Supported modes are `FM`, `NFM`, `AM`, `USB`, `LSB`, and `CW`. Channel frequencies are entered in MHz and stored exactly as integer Hz. CTCSS tones are entered in Hz and stored exactly as integer millihertz, preserving standard values such as `69.3 Hz` without floating-point math or rounding.

Tone syntax is normalized and exporter-neutral:

- **No tone:** `none`
- **CTCSS:** `ctcss:100.000`
- **DCS normal:** `dcs:023`
- **DCS inverted:** `dcs:023:inverted`

Updates change only explicitly supplied flags. Supplying an empty `--tx`, `--lat` and `--lon`, or `--tags` clears that field, and boolean flags can be turned off explicitly (`--skip=false`). `--tags +x,-y` adds `x` and removes `y`, leaving other tags alone; `--tags x,y` replaces them.

```sh
./rflib channel update --id <channel-id> --power low
./rflib channel status --id <channel-id> --set retired    # channels are retired, never deleted
```

**Location.** `--lat` and `--lon` say where a channel is used, such as a repeater's site, and `--radius-km` how far that reaches: either how rough the position is, or the repeater's coverage. Searches use it with `near:` (see [Search](search.md)). `--grid CM94sq` gives a Maidenhead grid square instead (4, 6, or 8 characters): its center, with a radius reaching its corners unless `--radius-km` says otherwise; a CSV import's `grid` column does the same. `--radius-km` alone changes the radius and keeps the coordinates; an empty `--lat` and `--lon` clear the location.

`--short-names` gives a channel your own abbreviations for radios with narrow displays, such as `--short-names "SB Sheriff Disp,SBSO Disp"`. See [Names on radio displays](names.md).

## Changing many channels at once

`channel update`, `channel status`, `radio channels remove`, and `radio channels set` take `--query` (`-q`) as well as `--id`. With both, a channel must match both. `--dry-run` lists the channels that would change, without changing them.

```sh
./rflib channel update -q band:noaa --tags +weather --skip
./rflib channel update -q 'service:none freq:150..174' --service public_safety --dry-run
./rflib channel status -q tag:old --set retired
./rflib radio channels set HT -q band:gmrs --power low
./rflib radio channels remove HT -q service:cb --yes    # or --all
```

- **Safe fields only:** updating many channels changes only `--tags`, `--skip`, `--power`, `--service`, `--rx-only`, and the location (`--lat` and `--lon` together, with `--radius-km` if wanted). Frequencies, tones, mode, and names change one channel at a time, by `--id`. Likewise, `radio channels set` sets `--name` one channel at a time.
- **Confirmation:** removing channels from a radio by query or `--all` needs `--yes`.
- **One transaction:** a bulk update or status change applies to every channel or to none.

## Output and pipes

List commands print a table. On a terminal it has aligned columns, a header, and IDs shortened to 8 characters. Piped, it prints plain tab-separated rows with no header and full IDs, so scripts get a stable format.

- **`--fields`** picks and orders the columns, such as `--fields name,rx,tone`. Each command's `--help` lists its fields. They are named like query fields, so a value can be copied into a search.
- **`--format tsv|csv|json`** chooses another format, and `--no-header` drops the header row. With `--format json` and no `--fields`, channel lists write each full record, as `show --format json` does; other lists write every field.
- **IDs:** any unique prefix of an ID, 4 characters or more, works wherever an ID does.
- **`--id -`** reads IDs from stdin, one per line, taking the first column. A list pipes straight into another command:

  ```sh
  ./rflib channel list band:2m is:repeater | ./rflib radio channels add HT --id -
  ```

- **stdout is data only.** Status messages ("Added 7 channels to HT"), warnings, and errors go to stderr. Commands that create something print its ID on stdout.

## Services and receive-only channels

Each channel can record its radio service with `--service`: `amateur`, `gmrs`, `frs`, `cb`, `murs`, `marine`, `noaa`, `public_safety`, or `other`. Leaving it out means unspecified; `rflib` never guesses from frequency, since services such as FRS and GMRS share channels. Search with `service:gmrs`, or `service:none` for channels without one.

`--rx-only` marks a channel you must never transmit on. Unlike omitting `--tx`, it keeps the system's input frequency on record (useful for public safety repeaters), while exports such as CHIRP program the channel receive only. `noaa` and `public_safety` channels are receive only unless you pass `--rx-only=false`.

```sh
./rflib channel add --name "SO Dispatch" --rx 460.275 --tx 465.275 --service public_safety
./rflib import chirp radioreference.csv --skip-invalid --service public_safety
```

To label channels you already imported, re-import the same file with `--on-duplicate update --service ...`. Service and receive-only don't count when matching duplicates, so the existing channels are updated in place.

### Band plan checks

Saving or importing a channel warns, without failing, when it doesn't fit its service's US (FCC) channel plan. That covers amateur transmit frequencies outside the amateur bands, off-plan FRS, GMRS, MURS, CB, and NOAA frequencies, repeater splits on simplex-only services, transmitting NOAA or public safety channels, and channels with no service that transmit outside the frequencies radios may transmit on (see [Where radios transmit](radios.md#where-radios-transmit)). `rflib check` reports every such channel in the library and exits non-zero when it finds any.

---

[Documentation index](README.md)
