# rflib

`rflib` is a local, SQLite-backed library for analog radio channels. It stores source information separately from radio-programming formats, so future exporters can adapt the same library to CHIRP and vendor-specific software.

## Build

```sh
make build    # or: go build -o rflib ./cmd/rflib
```

## Library location

Every command takes `--db PATH`. When it is omitted, `rflib` uses `$RFLIB_DB`, then `$XDG_DATA_HOME/rflib/library.db` (`~/.local/share/rflib/library.db` by default). The examples below set `RFLIB_DB` once instead of repeating `--db`.

The CLI only creates a database when you explicitly initialize one. After upgrading `rflib`, commands may report that the library schema is out of date; re-run `init` to upgrade it in place. Back up the database file first, since upgrades can convert data (for example, power text becoming [power intents](#power)).

```sh
export RFLIB_DB=./radio-library.db
./rflib init                 # a new database's shared list starts with the built-in lists
```

Back up the database by copying it while the CLI is not running.

## Your library and the shared list

A database holds **your library**, your own channels, radios, and saved searches, and the **shared list**, channels shared with everyone (see [Shared channels](#shared-channels)). Commands work on your library; the `rflib shared` commands work on the shared list. For a separate collection, such as a scratch space to try an import or someone else's radios, use a separate database file with `--db` or `$RFLIB_DB`.

## Shared channels

The `shared` library holds channels shared with everyone: the built-in lists, and the club's own. Your radios use shared channels directly, by their IDs, and follow their corrections, such as a repeater's new tone. Searches for radios (`radio channels add -q`, and radio sources) look at your own channels and the whole shared list.

```sh
./rflib channel list in:shared band:noaa            # search the whole shared list
./rflib radio channels add HT -q 'band:noaa'         # shared channels go straight on a radio
./rflib channel update --id <id> --notes "home QTH"  # your notes, tags, short names on a shared channel
./rflib shared link --dry-run                       # which of your channels match shared ones
./rflib shared link                                 # ...replace them with the shared ones
./rflib channel copy --id <id>                      # a private copy you can edit
```

- **One list:** `channel list` (and the web's Channels page) shows the channels you use: your own, and shared channels you've annotated or put on a radio. `in:shared` searches the whole shared list, `in:all` both, and `in:mine` only your own; in the web interface, the Source filter does the same. Shared channels are marked as such.
- **What's yours on a shared channel:** tags, notes, and short names. Power and scan are set on each radio (`radio channels set`). Everything else belongs to the shared channel, and editing it is refused: make a private copy with `channel copy`, which takes the shared channel's place on your radios and stops following changes. The shared channel's tags and notes are kept beside yours, and searches match both.
- **Names:** a shared channel shows the shared name. Rename it on a radio (`radio channels set --name`), or give it short names. Converting one of your channels keeps a name of your own as a short name.
- **Converting:** `shared link` replaces a private channel with the shared one it matches on the air (the same frequencies, tones, and tone squelch, and the same service, or none; FRS and GMRS share frequencies, so the service keeps them apart). The shared channel takes its place on your radios, its tags, notes, and name become yours on the shared channel, and the private channel goes. In the web interface, **Link to shared list** on the Channels page lists your matching channels and replaces the ones you pick.
- **Shared tags** come from a short list with agreed meanings: `rflib tag list`. None repeats a channel's fields (service, band, mode, or duplex), which search finds without a tag. Your own tags are free-form, and one with a shared tag's name means the same thing. Adding one of your own that repeats the channel's fields, such as `cb` on a CB channel, `ham` on an amateur one, or `repeater` on a repeater, is allowed but warns you that `service:cb` (and so on) already finds it.
- **The shared library is read-only** to everyday commands. Its maintainer changes it with `rflib shared import`, `update`, and `status`, each with a `--reason`.

### Status and changes

A shared channel is `active`, `off-air` (temporarily, such as for maintenance, with an expected-back date if known), or `retired` (gone for good, possibly replaced by another channel). A small change, such as a new tone, is a correction to the same channel; a repeater that becomes a different system is a new channel, and the old one is retired and points to it.

- **On your radios:** an off-air channel stays programmed, so it needs no reprogramming when it's back, but it isn't scanned. Retired channels warn on export.
- **`rflib shared changes`** lists the shared channels you use (on your radios, or with your notes and tags) that changed since you last looked: what changed, why, and the radios they're on, which need programming again. `rflib shared ack --all` marks them seen. `rflib shared relink --all` moves retired channels' places on your radios, and your notes and tags, over to their replacements.
- **History:** every change to a shared channel is kept, with when and why: `rflib shared history --id <id>`.
- **Searching:** `status:off-air` finds channels that are down.

```sh
./rflib shared status --id <id> --set off-air --note "antenna work" --back 2026-12-01 --reason "owner reported"
./rflib shared status --id <id> --set retired --replaced-by <new-id> --reason "moved to Diablo Peak"
./rflib shared update --id <id> --tx-tone ctcss:131.8 --reason "new tone"
./rflib shared import csv club-repeaters.csv --reason "club list, October"
```

## Channels

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

See [Search](#search) for the query language.

Supported modes are `FM`, `NFM`, `AM`, `USB`, `LSB`, and `CW`. Channel frequencies are entered in MHz and stored exactly as integer Hz. CTCSS tones are entered in Hz and stored exactly as integer millihertz, preserving standard values such as `69.3 Hz` without floating-point math or rounding.

Tone syntax is normalized and exporter-neutral:

- **No tone:** `none`
- **CTCSS:** `ctcss:100.000`
- **DCS normal:** `dcs:023`
- **DCS inverted:** `dcs:023:inverted`

Updates change only explicitly supplied flags. Supplying an empty `--tx`, `--lat` and `--lon`, or `--tags` clears that field, and boolean flags can be turned off explicitly (`--skip=false`). `--tags +x,-y` adds `x` and removes `y`, leaving other tags alone; `--tags x,y` replaces them.

```sh
./rflib channel update --id <channel-id> --power low
./rflib channel delete --id <channel-id> --yes
```

**Location.** `--lat` and `--lon` say where a channel is used, such as a repeater's site, and `--radius-km` how far that reaches: either how rough the position is, or the repeater's coverage. Searches use it with `near:` (see [Search](#search)). `--radius-km` alone changes the radius and keeps the coordinates; an empty `--lat` and `--lon` clear the location.

**Groups are gone.** Earlier versions collected channels into groups. Upgrading (`rflib init`) gives each grouped channel its group's location and power, where it had none, and a tag named after the group. Saved searches and radio sources that used `group:NAME` now say `tag:NAME`, and `group:NAME` still works as `tag:NAME`. Old backups and CSV files with groups import the same way.

`--short-names` gives a channel your own abbreviations for radios with narrow displays, such as `--short-names "SB Sheriff Disp,SBSO Disp"`. See [Names on radio displays](#names-on-radio-displays).

### Changing many channels at once

`channel update`, `channel delete`, `radio channels remove`, and `radio channels set` take `--query` (`-q`) as well as `--id`. With both, a channel must match both. `--dry-run` lists the channels that would change, without changing them.

```sh
./rflib channel update -q band:noaa --tags +weather --skip
./rflib channel update -q 'service:none freq:150..174' --service public_safety --dry-run
./rflib channel delete -q tag:old --yes
./rflib radio channels set HT -q band:gmrs --power low
./rflib radio channels remove HT -q service:cb --yes    # or --all
```

- **Safe fields only:** updating many channels changes only `--tags`, `--skip`, `--power`, `--service`, `--rx-only`, and the location (`--lat` and `--lon` together, with `--radius-km` if wanted). Frequencies, tones, mode, and names change one channel at a time, by `--id`. Likewise, `radio channels set` sets `--name` and `--section-start` one channel at a time.
- **Confirmation:** deleting channels always needs `--yes`, and so does removing channels from a radio by query or `--all`.
- **One transaction:** a bulk update or delete applies to every channel or to none.

### Output and pipes

List commands print a table. On a terminal it has aligned columns, a header, and IDs shortened to 8 characters. Piped, it prints plain tab-separated rows with no header and full IDs, so scripts get a stable format.

- **`--fields`** picks and orders the columns, such as `--fields name,rx,tone`. Each command's `--help` lists its fields. They are named like query fields, so a value can be copied into a search.
- **`--format tsv|csv|json`** chooses another format, and `--no-header` drops the header row. With `--format json` and no `--fields`, channel lists write each full record, as `show --format json` does; other lists write every field.
- **IDs:** any unique prefix of an ID, 4 characters or more, works wherever an ID does.
- **`--id -`** reads IDs from stdin, one per line, taking the first column. A list pipes straight into another command:

  ```sh
  ./rflib channel list band:2m is:repeater | ./rflib radio channels add HT --id -
  ```

- **stdout is data only.** Status messages ("Added 7 channels to HT"), warnings, and errors go to stderr. Commands that create something print its ID on stdout.

## Search

`channel list` takes a query, much like a Gmail search. Every command that selects channels (`export`, `names`, `check`, and `radio channels add`) takes the same query with `--query` or `-q`.

```sh
./rflib channel list service:none freq:144..148 is:repeater
./rflib channel list 'tag:ham (near:34.64,-120.46,40 OR -has:location)'
./rflib export chirp -q 'band:2m OR band:70cm' --output dual.csv
```

- **Combining:** terms are `field:value`, and a space between them means AND. `OR` binds tighter than AND, so `a b OR c` means `a AND (b OR c)`.
- **Negating:** a leading `-` (or `NOT`) negates a term or a parenthesized group. When a negation comes first among the arguments, put `--` before it so it isn't read as a flag: `channel list -- -tag:hf`. Quoting a whole query, or passing it with `-q`, avoids this.
- **Grouping and quoting:** parentheses group terms, and quotes keep spaces in a value: `name:"Lompoc PD"`.
- **Bare words** match names, short names, and notes, ignoring case.

| Field | Values |
|---|---|
| `name:`, `notes:` | Text the name (or a short name), or the notes, contains |
| `service:` | A service, or `none` |
| `tag:` | A tag |
| `freq:`, `rx:`, `tx:` | MHz: `146.52`, a range `144..148` (either end optional), or `>=420`, `<30`. `freq:` matches either frequency. |
| `band:` | `160m` through `23cm`, `air`, `marine`, `cb`, `frs`, `gmrs`, `murs`, `noaa` (by receive frequency) |
| `duplex:` | `+`, `-`, `split`, `simplex`, or `off` (no transmit frequency) |
| `tone:`, `txtone:`, `rxtone:` | `131.8`, `dcs:023`, `ctcss`, `dcs`, or `none`, as programmed: the receive tone counts only with tone squelch. `tone:` matches either direction; `tone:none` means neither has one. |
| `mode:` | `FM`, `NFM`, `AM`, `USB`, `LSB`, or `CW` |
| `power:` | `low`, `medium`, `high`, `max`, or `none` |
| `squelch:`, `skip:` | `yes` or `no` |
| `is:` | `repeater` (transmit differs from receive), `simplex`, `rxonly`, `transmit` (a radio export would let it transmit; see [Where radios transmit](#where-radios-transmit)), `skip` |
| `has:` | `location`, `service`, `tag`, `tone`, `power`, `notes`, `shortname`, `radio`, `network`, `org` |
| `near:` | `LAT,LON,KM`, see below |
| `radio:` | A radio's name, `any`, or `none` |
| `tunes:` | A radio or model name: channels in its bands and in a mode it has |
| `created:`, `updated:` | `YYYY-MM-DD`, with comparisons and ranges like `freq:` |
| `id:` | A channel ID |
| `status:` | `active`, `off-air`, or `retired` (see [Status and changes](#status-and-changes)) |
| `in:` | `use` (the default): your own channels and the shared channels you annotate or have on a radio; `mine`: only your own; `shared`: the whole shared list; `all`: both. Only at the top level of a query. Radio sources and adding to a radio search your own channels and the whole shared list unless a query says otherwise. |
| `org:` | An organization's name or ID: channels it operates |
| `system:` | A system's name or ID: its members, or links to them |
| `allstar:`, `echolink:`, `irlp:`, `wires-x:` | A node number on that network |

**Location.** `near:` matches when a channel's location is within the distance plus the channel's own `--radius-km`, so a repeater whose coverage reaches you counts. Channels without a location never match `near:`. To keep them, say so: `near:34.64,-120.46,40 OR -has:location`.

**Shorthand flags.** `--tag`, `--service`, and `--lat`/`--lon`/`--radius-km` still work and are added to the query.

### Saved searches

Save a query by name and use it anywhere as `@name`: in `channel list`, in other searches, and in radio sources.

```sh
./rflib search add local-rptrs '(band:2m OR band:70cm) is:repeater near:34.64,-120.46,50'
./rflib channel list @local-rptrs -tag:hf
./rflib search list          # each search, how many channels it matches, and its query
./rflib search show local-rptrs
./rflib search update local-rptrs --query 'band:2m is:repeater'
```

A search can't use itself, directly or through others. A search in use by another search or a radio can't be renamed or deleted; `search show` lists what uses it.

## Services and receive-only channels

Each channel can record its radio service with `--service`: `amateur`, `gmrs`, `frs`, `cb`, `murs`, `marine`, `noaa`, `public_safety`, or `other`. Leaving it out means unspecified; `rflib` never guesses from frequency, since services such as FRS and GMRS share channels. Search with `service:gmrs`, or `service:none` for channels without one.

`--rx-only` marks a channel you must never transmit on. Unlike omitting `--tx`, it keeps the system's input frequency on record (useful for public safety repeaters), while exports such as CHIRP program the channel receive only. `noaa` and `public_safety` channels are receive only unless you pass `--rx-only=false`.

```sh
./rflib channel add --name "SO Dispatch" --rx 460.275 --tx 465.275 --service public_safety
./rflib import chirp radioreference.csv --skip-invalid --service public_safety
```

To label channels you already imported, re-import the same file with `--on-duplicate update --service ...`. Service and receive-only don't count when matching duplicates, so the existing channels are updated in place.

### Band plan checks

Saving or importing a channel warns, without failing, when it doesn't fit its service's US (FCC) channel plan. That covers amateur transmit frequencies outside the amateur bands, off-plan FRS, GMRS, MURS, CB, and NOAA frequencies, repeater splits on simplex-only services, transmitting NOAA or public safety channels, and channels with no service that transmit outside the frequencies radios may transmit on (see [Where radios transmit](#where-radios-transmit)). `rflib check` reports every such channel in the library and exits non-zero when it finds any.

## Built-in channel lists

```sh
./rflib seed list                    # each list, and whether the shared list is up to date
./rflib seed show us-gmrs
./rflib seed update --dry-run        # what a newer rflib's lists would change
./rflib seed update                  # apply it
```

| List | Channels |
|---|---|
| `us-frs` | FRS 1-22, with power limits in the notes |
| `us-gmrs` | GMRS 1-22 plus repeater pairs 15R-22R (add your repeater's tone) |
| `us-murs` | MURS 1-5 |
| `us-cb` | CB 1-40, AM |
| `us-noaa` | NOAA Weather Radio WX1-WX7, receive only |
| `us-ham-calling` | Amateur FM and SSB simplex calling frequencies, 10 m through 70 cm |
| `us-aprs` | APRS on 144.390, and the ISS and satellite digipeater on 145.825 |
| `us-iss` | ISS voice (145.800 down, 144.490 up) and its cross-band repeater |
| `us-ft8` | FT8 dial frequencies, 160 m through 2 m, USB (60 m FT8 is channel 3 in `us-60m`) |
| `us-ft4` | FT4 dial frequencies, 80 m through 2 m, USB |
| `us-js8` | JS8Call dial frequencies, 160 m through 2 m, USB (17 m and 6 m share FT4's dials, in `us-ft4`) |
| `us-wspr` | WSPR dial frequencies, 160 m through 23 cm, USB |
| `us-psk31` | PSK31 activity frequencies, 160 m through 10 m, USB (17 m shares FT8's dial, in `us-ft8`) |
| `us-sstv` | Analog SSTV calling frequencies, 80 m through 10 m |
| `us-60m` | Amateur 60 m channels 1-5, USB dial frequencies |
| `us-beacons` | NCDXF/IARU HF beacons, 20 m through 10 m, CW, receive only |
| `us-fm-sats` | FM amateur satellites (SO-50, AO-123, AO-91, PO-101, CAS-3H) with uplink tones; expect Doppler shift |
| `us-time` | WWV and CHU time signals, receive only |
| `us-radiofax` | NWS HF radiofax (weather fax) stations, USB dial frequencies, receive only |
| `us-interop` | National public-safety interoperability calling and tactical channels (VHF, UHF, and 800 MHz), receive only |

Built-in lists fill the shared list; your library takes channels from there (see [Shared channels](#shared-channels)). A new database's shared list starts with every list (`rflib init --no-seed` starts it empty). Upgrading rflib never changes the shared list: when a newer version changes a list, `seed list` shows the changes pending, and `seed update` applies them, as the maintainer chooses. An update adds the list's new channels, applies what the list changed since it was last applied, and retires channels it dropped. A field the maintainer corrected keeps the correction, and the update reports it. Each change is in the channel's history, with the list as its reason. The lists carry only shared tags, such as `calling`; the old `gmrs`, `noaa`, and similar tags repeated the service and band. In the web interface, **Built-in lists** is under the Shared list in the navigation.

## Radios

A radio is one of your own radios: a model, the channels programmed into it in your order, and per-radio choices. Build each radio from the shared library, then export its programming file.

```sh
./rflib model list
./rflib radio add Truck --model yaesu-ftm150 --default-power medium
./rflib radio channels add Truck -q tag:mobile --sort freq
./rflib radio channels add Truck --id <channel-id>
./rflib radio channels list Truck
./rflib radio export Truck --output truck.csv
```

### Models

| Model | Programmed with | Memories | Name length | Power levels | Modes |
|---|---|---|---|---|---|
| `yaesu-ftm150` | ADMS-17 (`yaesu-ftm150` profile) | 1-999 | 16 | LOW 5 W, MID 25 W, HIGH 50 W | FM, NFM, AM |
| `yaesu-ftm6000` | ADMS-13 (`yaesu-ftm6000` profile) | 1-999 | 6 | LOW 5 W, MID 25 W, HIGH 50 W | FM, NFM, AM |
| `yaesu-ft857` | CHIRP: Yaesu FT-857/897 (US) | 1-200 | 8 | set from the radio's menu | FM, NFM, AM, USB, LSB, CW |
| `tidradio-tdh3-gmrs` | CHIRP: TIDRADIO TD-H3-GMRS | 1-199 | 8 | Low 2 W, High 5 W | FM, NFM, AM |
| `baofeng-uv5rm` | CHIRP: Baofeng 5RM | 1-1000 | 12 | Low 1 W, Medium 5 W, High 8 W | FM, NFM, AM |
| `explorer-qrz1` | CHIRP: Explorer QRZ-1 | 1-200 | 10 | Low 0.5 W, Mid 2.5 W, High 5 W | FM, NFM |
| `tyt-th9800` | CHIRP: TYT TH-9800 | 1-800 | 6, upper case | Low 5 W, Mid2 10 W, Mid1 20 W, High 50 W | FM, NFM, AM |
| `radioddity-uv5g` | CHIRP: Radioddity UV-5G | 0-127 | 7, upper case | Low 1 W, High 4 W | FM, NFM |
| `chirp-generic` | CHIRP: any radio it supports | 1-999 | unlimited | left to CHIRP | not checked |

`rflib model show NAME` prints a model's full facts, including allowed name characters, tunable bands, modes, and where the facts came from: CHIRP's driver source, or the radio's operating manual. `rflib model list` also lists your own models and the directory they're read from.

### Radios CHIRP supports but rflib doesn't

**The quick way:** use `chirp-generic` and tell rflib how long a name the radio shows.

```sh
./rflib radio add HT --model chirp-generic --name-length 7
```

Nothing else is checked: rflib doesn't know the radio's bands, modes, or memory count, so make sure its channels suit it. Export the file, then in CHIRP download your radio's image and import the file into it. CHIRP drops characters the radio can't show, and since power is left blank, it picks the radio's middle or highest power level.

**A model of your own** gets rflib's checks and per-memory power. `rflib model new` copies `chirp-generic` (or `--from` another model) into your models directory; edit it to match your radio, then check it:

```sh
./rflib model new my-ht            # prints the new file's path
./rflib model validate ~/.config/rflib/models/my-ht.yaml
./rflib radio update HT --model my-ht
```

To fill it in from CHIRP itself, `tools/chirp_model.py` drafts a model from CHIRP's driver: memories, name length and characters, power levels with watts, bands, and modes. It needs [uv](https://docs.astral.sh/uv/), which fetches CHIRP the first time:

```sh
uv run tools/chirp_model.py --list uv-5r                     # find the radio as CHIRP names it
uv run tools/chirp_model.py "Baofeng UV-5R" > ~/.config/rflib/models/baofeng-uv-5r.yaml
./rflib model validate ~/.config/rflib/models/baofeng-uv-5r.yaml
```

Check the draft against the radio's manual or a CHIRP export of your radio: some drivers depend on the radio's region or firmware, which the draft can't see. Optionally tag power levels with an `intent` (low, medium, high, or max) to choose which level each power setting uses.

**Share it:** once a model works on your radio, [submit it](https://github.com/rgeyer/rflib/issues/new?template=radio-model.yml) with a sample CHIRP export, so others get it built in.

### Channel order and memory numbers

`radio channels add` appends to the end of the list, by `--id` or by `--query`. Channels the radio can't receive are skipped and listed on stderr: those outside its model's bands (`--any-band` adds them anyway), and those in a mode it doesn't have, such as USB on an FM handheld (`--any-mode`). `channel list tunes:Truck` previews what fits. A channel in a mode the radio lacks that is on it anyway, added with `--any-mode` or before modes were checked, is listed with a warning but left out of the export. Channels already on the radio are skipped, so adding never reshuffles anything. Rearrange the list at any time:

```sh
./rflib radio channels move Truck --id <id> --before <other-id>   # or --after, --to N, --top, --bottom
./rflib radio channels remove Truck --id <id>    # or -q QUERY --yes, or --all --yes
```

Memory numbers follow the list from the model's first memory. To lay a radio out in banks, start a section at a chosen memory number; the channels after it continue from there:

```sh
./rflib radio channels set Truck --id <first-gmrs-id> --section-start 100
```

A section start can't go backwards, and the list must fit the model's memories. Deleting a channel from the library takes it off every radio.

### Per-radio settings

`radio channels set` overrides a channel on one radio only. An empty value removes the override, so the channel's own setting applies again. `--power` and `--scan` can be set on many channels at once with `-q` (see [Changing many channels at once](#changing-many-channels-at-once)).

| Flag | Overrides |
|---|---|
| `--name` | The name on this radio (still fitted to its display) |
| `--power low\|medium\|high\|max` | The power on this radio |
| `--scan yes\|no\|default` | Whether this radio scans the channel. The channel's `--skip` is the default. |
| `--section-start N` | Starts a section at memory N |

`radio channels list Truck` shows every memory as it will be programmed: number, name on the radio and how it was fitted, power level, scan, frequency, duplex and offset, tone, and the library channel's name when the radio's differs. `§` marks a section start, and `*` marks a radio override. Tones read as repeater lists write them: `T131.8` sends a tone, `TSQL 131.8` also squelches on it, and `T131.8 R88.5` uses different ones; DCS codes read `D023N` or `D023I`. Warnings, such as a power cap or a frequency outside the radio's bands, go to stderr, and `radio export` repeats them.

Each radio has its own `--dedupe` (`none`, `index`, or `freq`) for duplicate names, and `--name-length` for a radio whose model doesn't know how long a name it shows, such as `chirp-generic` (0 uses the model's); see [Names on radio displays](#names-on-radio-displays).

### Where radios transmit

A radio's memories transmit only where a ham or personal-radio user may: the US amateur bands, FRS and GMRS channels (plus GMRS repeater inputs), MURS, and CB. Any other channel is programmed receive-only, with a warning, whatever its service or receive-only setting says. A public safety repeater pair imported without `--service` can therefore never transmit on its input.

On CHIRP radios, receive-only means duplex `off`. The Yaesu FTM-150 and FTM-6000 have no per-memory transmit inhibit, so those memories are written as simplex, and the radios themselves refuse to transmit outside 2 m and 70 cm.

### Keeping radios up to date

A radio's **sources** are searches that `radio sync` adds new matching channels from. Each source keeps its channels together in the radio's list, in its own sort order, and can start a section at a memory number.

```sh
./rflib radio sources add HT @local-rptrs --sort freq
./rflib radio sources add HT band:gmrs --sort freq --section-start 100
./rflib radio sources add HT band:noaa --section-start 150
./rflib radio sync HT --dry-run
./rflib radio sync HT
./rflib radio sources list HT    # number, section start, sort, channels added, query
```

- **Placement:** each source's new channels go after the last channel that source added. A source with none yet goes after the channels of earlier sources and before those of later ones.
- **Order and claims:** sources are numbered from 1. An earlier source claims a channel first, and `radio sources move HT 3 --to 1` changes the order.
- **Bands and modes:** like `radio channels add`, sync skips channels outside the radio's bands or in modes it lacks, reports how many, and takes `--any-band` and `--any-mode`.
- **What sync leaves alone:** it never removes anything, and channels already on the radio keep their places unless re-sorted (below). Channels you add by hand stay where you put them. Moving a source's channel by hand (`radio channels move`) makes it yours: it keeps its new place and no longer counts as the source's.
- **Re-sorting:** `radio sync HT --resort` also puts each source's channels in its sort order, within the places in the list they already hold. Channels added or moved by hand keep their places, and section starts stay put. Memory numbers change, so program the radio again afterwards; `--dry-run --resort` counts what would move. To re-sort a source on every sync, set it with `radio sources set HT 2 --keep-sorted` (or `--keep-sorted` on `radio sources add`).
- **Changing a source:** `radio sources set HT 2 --query ... --sort ... --section-start ... --keep-sorted` changes a source. A new section start moves to the source's first channel right away.
- **Removing a source:** `radio sources remove HT 2` removes a source and leaves its channels on the radio. Add `--with-channels` to take them off too.
- **Plans:** a source can be a plan instead of a query, and then follows the plan; see [Plans](#plans).

Sync warns when the result no longer fits the radio, such as a source running past the next section start.

## Organizations, systems, and plans

The shared list also records who runs its repeaters, how they're linked, and the communications plans that use them. All three live in the shared library, so the commands work the same from any library. Changes are maintainer operations: each takes a `--reason`, and every change is kept in a history (`rflib org|system|plan history NAME`).

### Network nodes

A channel can carry its nodes on linking networks: AllStar, EchoLink, IRLP, and WIRES-X. Search them with `allstar:43761`, `echolink:`, `irlp:`, and `wires-x:`, or `has:network`. The web interface links each node to its network's own pages; rflib fetches nothing from the networks.

```sh
./rflib channel update --id <id> --networks allstar:43761,echolink:12345
./rflib shared update --id <id> --networks allstar:43761 --reason "node numbers"
```

The shared tags `linked`, `allstar`, `echolink`, and `wires-x` are gone: network nodes and systems say it better. So is `simplex`, which `is:simplex` finds. Upgrading (`rflib init`) takes them off shared channels; your own tags stay.

### Organizations

Organizations are who runs or supports repeaters and systems: clubs, emergency services, agencies, and individuals. They're plain records, attached to shared channels and systems as an **operator** or a **partner** (such as a site's co-sponsor). `org:NAME` finds the channels an organization operates.

```sh
./rflib org add SBARC --callsign W6SBA --website https://www.sbarc.org --reason "pilot"
./rflib shared update --id <id> --operators SBARC --partners GRONK --reason "who runs it"
./rflib org show SBARC      # its channels and systems
./rflib org list
```

An organization attached to anything can't be deleted.

### Systems

A system is a set of interconnected channels, such as a club's linked repeaters, as its operators intend it: not its live connections. Members are shared channels, linked **full-time** (usually linked) or **on demand** (may be linked), with a note on how. A system can have nodes of its own, such as a hub's AllStar node, and its members can belong to different organizations. `system:NAME` finds a system's channels, in your library through your links.

```sh
./rflib system add "SBARC Hub" --link allstar --networks allstar:43763 --operators SBARC --reason "pilot"
./rflib system members add "SBARC Hub" --id <id>,<id> --reason "pilot"
./rflib system members add "SBARC Hub" --id <id> --on-demand --notes "microwave to the hub" --reason "pilot"
./rflib system show "SBARC Hub"
./rflib channel list 'system:"SBARC Hub"'
```

The map draws lines between a system's members, dashed for members linked on demand.

### Plans

A plan is a communications plan: an ordered selection of shared channels, each with its name and assignment in the plan, laid out as ICS 205 lists them. An event is a plan with dates. A plan is `draft`, `published`, or `archived`, and is never deleted.

```sh
./rflib plan add "Fair 2026" --starts 2026-07-15 --ends 2026-07-26 --reason "county fair"
./rflib plan entries add "Fair 2026" --id <id> --name "FAIR TAC1" --assignment "Tactical" --reason "plan"
./rflib plan entries add "Fair 2026" -q 'system:"SBARC Hub"' --section-start 10 --assignment "Wide area" --reason "plan"
./rflib plan channels "Fair 2026"     # Ch #, name, assignment, frequencies, tones, mode, remarks
./rflib plan show "Fair 2026"         # the entries, numbered for entries move, set, and remove
./rflib plan update "Fair 2026" --status published --reason "ready"
```

- **Entries** are shared channels, renamed for the plan if you like (147.420 simplex as "FAIR TAC1", so plans rarely need new shared channels), or **query sections**, which bring in every shared channel a query matches, in name or frequency order, leaving out channels the plan lists elsewhere.
- **Numbers:** a plan numbers its channels from 1 (ICS 205's Ch #); `--section-start N` starts an entry at channel N.

**On a radio**, a plan is a source: `rflib radio sources add HT --plan "Fair 2026" --section-start 50`. Its part of the radio follows the plan:

- `radio sync` puts the plan's channels on the radio in the plan's order with the plan's names, and numbers them as the plan does from the section start: plan channel 10 becomes memory 59 when the source starts at 50.
- Plans claim their channels before query sources, and a plan's channel that's elsewhere on the radio moves into the plan's part.
- Channels the plan drops come off, and removing the source takes all of its channels off.
- An archived plan's channels stay as they are.

## Power

Power is an intent rather than a number: `low`, `medium`, `high`, or `max`. Each radio model maps it to its own levels, so "high" is 50 W on the FTM-150 and 5 W on the QRZ-1.

For each memory, the first of these that is set wins:

1. the radio's override for that channel (`radio channels set --power`)
2. the channel's power (`channel add/update --power`)
3. the radio's default (`radio add/update --default-power`)

If none is set, the memory gets `high`: a radio with power levels needs one on every memory, and CHIRP rejects a blank one. Set the radio's default to choose otherwise.

If a model has no level for the intent, `low` takes its lowest level, and `medium`, `high`, and `max` take its high level. A missing middle level errs toward being heard.

**Service limits.** FRS, GMRS, MURS, and CB channels are held to their US limits: FRS 2 W (0.5 W on channels 8-14), GMRS 5 W on channels 1-7, 0.5 W on 8-14, and 50 W otherwise, MURS 2 W, and CB 4 W. When no power is set, the radio's highest level within the limit is used rather than `high`. When even the lowest level is over the limit, or the model doesn't know its watts, the export warns.

**In the files:**
- **CHIRP radios** get the level's watts in CHIRP's Power column, which is the only form CHIRP accepts, and CHIRP picks the radio's nearest level.
- **ADMS radios** get the level name (`HIGH`, `MID`, `LOW`).
- **Exports without a radio** (`export chirp`, `export profile`) have no model to map through. CHIRP's Power column is left blank, which some CHIRP radios reject; export through a radio (`radio export`) for those. Profiles map the intent name.

**Older power text.** Power written as text before this version is migrated: HIGH, MID, LOW, and similar become intents, and anything else, such as `50W`, is dropped. Imports read power the same way. Watts in a RadioReference or CHIRP file describe the source's radio, not yours.

## Export

Exports write to stdout unless `--output PATH` is given, and select channels with `--query` (see [Search](#search)).

```sh
./rflib export csv --output channels.csv --tag ham
./rflib export csv near:45.52,-122.68,40 > metro.csv
./rflib export json --output backup.json
```

- **`export csv`** is deliberately vendor-neutral: stable headers for the library's normalized channel data, including receive/transmit frequencies, tones, tags, notes, service, receive-only, short names, and location. New columns are only ever added at the end; the group columns were removed when groups were.
- **`export chirp`** writes a file for CHIRP's *File > Import*. See [CHIRP](#chirp) below.
- **`export profile NAME`** writes a radio programming tool's own CSV format, such as Yaesu ADMS. See [Export profiles](#export-profiles) below.
- **`export json`** is a full-fidelity backup, including IDs and timestamps, which the CSV leaves out. Your notes, tags, and short names on shared channels are kept, with the shared channel as it was; restored into a database without that shared channel, it becomes a private copy. `rflib shared export json` backs up the shared list, with its organizations, systems, and plans, and `rflib shared import json` restores it. The CSV writes a shared channel's tags and notes and yours together.

## Import

```sh
./rflib import csv channels.csv --dry-run
./rflib import csv channels.csv --tags imported
./rflib import json backup.json
./rflib import chirp my-radio.csv --tags handheld
./rflib export csv --tag ham | ./rflib --db other.db import csv
```

`import csv` reads the generic CSV format. Columns are matched by header name, so they can be in any order and extra columns are ignored. Only `name` and `receive_mhz` are required, which makes hand-built spreadsheets easy to load. `import json` restores a backup exactly. A link stays a link when its shared channel is in this database's shared list; otherwise it becomes a private copy, with a warning. Imported channels are always private and active. `import chirp` reads a file saved with CHIRP's *File > Export*.

- **All or nothing:** any invalid row is reported with its line number and nothing is imported. Use `--skip-invalid` to import the valid rows anyway. A failure partway through also writes nothing.
- **`--dry-run`** runs the whole import and then discards it, so its summary is exactly what a real import would do.
- **Duplicates:** a channel is a duplicate when it has the same ID as an existing channel, or the same name, receive and transmit frequencies, tones, and tone squelch. Channels that only share a name and frequency, such as two sites with different tones, are kept apart. `--on-duplicate` chooses `skip` (the default), `update`, or `create`. `--match programmed` drops the name from the comparison and matches on frequencies, tones, and tone squelch alone. Use it for a file read from a radio, where names are shortened. With `--on-duplicate update`, the matched channel takes the file's name. Two rows in the same file with the same programming also match each other. `update` changes details like power, notes, skip, and tags; a different tone or frequency counts as a new channel. Re-running an import is therefore safe.
- **Old files with groups:** a group becomes a tag named after it, and its coordinates the channel's location (see [Channels](#channels)).
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
| `yaesu-ftm150` | ADMS-17 | 16 characters | FM, NFM, AM |
| `yaesu-ftm6000` | ADMS-13 | 6 characters | FM, NFM, AM |

In ADMS, open the *Memories* tab and use *File > Import* on the exported file. As ADMS's own exports do, the file holds all 999 memories: your channels first, then empty ones.

- **Power:** exporting a radio writes its level (`HIGH`, `MID`, `LOW`; see [Power](#power)). Exporting channels directly maps their power intent, and channels without one get `HIGH`.
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
| `name`, `notes`, `service`, `tags` | Channel fields (notes on one line, tags joined with `;`) |
| `rx_mhz`, `tx_mhz`, `offset_mhz` | Frequencies in MHz. `tx_mhz` is the receive frequency for receive-only channels. |
| `duplex` | `none`, `plus`, `minus`, `split`, or `off` |
| `mode`, `narrow` | `FM`/`NFM`/`AM`/`USB`/`LSB`/`CW`; `true` for NFM |
| `tone_mode` | `none`, `tone`, `tsql`, `dcs`, `dcs_enc`, `tone_dcs`, `dcs_tone`, `tone_tone`, `dcs_dcs`, `rx_tone`, or `rx_dcs` |
| `ctcss_hz`, `tx_ctcss_hz`, `rx_ctcss_hz` | CTCSS tones in Hz |
| `dcs_code`, `tx_dcs_code`, `rx_dcs_code`, `dcs_polarity` | DCS codes, and polarity as `NN`/`NR`/`RN`/`RR`. Without a polarity column, inverted DCS can't be exported. |
| `power`, `skip` | When exporting a radio, its power level label; otherwise the power intent (`LOW`, `MEDIUM`, `HIGH`, `MAX`). `skip` is `true` when skipped. |
| `step_khz` | First of the column's `steps_khz` that the frequency fits |
| `const` | The column's `value` |

## CHIRP

```sh
./rflib export chirp --tag ham --sort freq --max-name-len 8 --output ft60.csv
```

For your own radios, `radio export` is simpler: it knows the model, your order, power, and name length. `export chirp` exports a filtered set of channels without a radio.

Open the radio's image in CHIRP, then use *File > Import* on the exported file. Memories are numbered from `--start` (default 1) in `--sort` order: `name` (the default) or `freq`. The `name` order compares numbers in names by value, so "GMRS 2" comes before "GMRS 10", and "GMRS 15" before "GMRS 15R". Use `--max-name-len` to fit names to the radio's display (see [Names on radio displays](#names-on-radio-displays)).

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
| Power | Watts of the radio's level when exporting a radio, otherwise blank (see [Power](#power)) |
| Notes | Comment |

The receive tone is only programmed when tone squelch is on, since that is what makes the radio use it.

CHIRP export accepts every library mode, but only the 50 standard CTCSS tones and the 104 standard DCS codes. An export that includes anything else fails and names every such channel. Add `--skip-invalid` to leave them out with a warning instead.

`import chirp` does the reverse. Memory locations aren't kept, unnamed memories are named after their frequency, and memories in modes the library doesn't support yet (digital modes such as DV, DMR, and P25) are reported as invalid rows. Pass `--service` to label everything you import.

## Web interface

`rflib serve` also serves a web interface at `http://127.0.0.1:8080/`, built into the binary:

- **Channels:** search with the query language (the Band, Service, Type, Tone, Status, and Tag buttons write their terms into the search for you), add and edit channels (with a tone picker), select several to tag, set their service, delete, or add to a radio, and choose Source to search the whole shared list as well as the channels you use. A globe marks a shared channel, and a note icon your notes or tags on one.
- **Shared list:** opened from its own item in the navigation. As the maintainer, open a channel to correct it (its frequencies, tones, location, curated tags, notes, organizations, and nodes) or to set its status (off the air with a note and an expected-back date, retired with its replacement, or active). Each correction asks for a reason and reaches everyone who uses the channel. Corrections are made one channel at a time.
- **Map:** channels with a location, and how far each reaches. The map background comes from OpenStreetMap, so it needs an internet connection; everything else works without one.
- **Radios:** each radio's memories as they'll be programmed, with warnings, scan switches, and moves. Search them as you would channels, filter by this radio's own settings (scan, power, overrides, how the name fit, and memories left out of the export), and select several (shift-click selects a range) to set scan or power, or take them off the radio; its sources, with a preview of what a new source would add before you add it; sync with a preview; and the programming file to download. On the Channels page, **Add search as source** puts the current search on a radio as a source, with the same preview.
- **Systems, Plans, and Organizations:** the shared list's systems with a map of their links, plans laid out as ICS 205 with their entries to edit, and organizations with what they run. Each change asks for a reason and shows in the page's history. A radio's Sources tab adds a plan as a source.
- **Import:** a file, its options, a dry-run preview, then the import.
- **Built-in lists** (under the Shared list, for the maintainer) and **shared changes**, as `rflib seed update` and `rflib shared changes` do. Each list shows whether the shared list is up to date with it; reviewing an update lists the channels to add, update, and retire, and any correction it keeps.

The switch at the top moves between **My library** (the default library) and the **Shared list**. Once you have more than one library of your own, it becomes a list of their names.

When the server needs a token (`$RFLIB_API_TOKEN`), the page asks for it once and keeps it in the browser.

Building the interface needs Node.js: `make web` builds it into `web/dist`, and `make build` then embeds it. An rflib built without it serves a page saying so. See [Development](#development).

## HTTP API

`rflib serve` offers everything above as a JSON API, for scripts and the web interface.

```sh
./rflib serve                                    # http://127.0.0.1:8080/api/v1
RFLIB_API_TOKEN=s3cret ./rflib serve --addr :8080
curl -H "Authorization: Bearer s3cret" 'localhost:8080/api/v1/libraries/default/channels?q=band:2m%20is:repeater'
```

- **The description:** `GET /api/v1/openapi.yaml` is the OpenAPI document, which lists every endpoint and shape. Its source is `internal/api/openapi.yaml`, and the tests hold every route and response to it.
- **Authentication:** with `$RFLIB_API_TOKEN` set, every request but `/health` and the document needs `Authorization: Bearer TOKEN`. Serving on anything but a loopback address requires a token.
- **Resources:** everything lives under `/api/v1/libraries/{lib}/`: `channels` (with `bulk-update`, `bulk-delete`, and `copy`; a shared channel's ID gives your view of it, and saving it saves your notes, tags, and short names), `radios` (memories, channels, overrides, sources, sync, and export), `searches`, `imports/{format}`, `exports/{format}`, `shared` (convert, changes, ack, and relink), and `feed`. `organizations`, `systems`, and `plans` (each with `history`), which live in the shared list, and `models`, `profiles`, `seeds` (with `update`), and `tags` sit beside `libraries`.
- **Values:** frequencies are exact integer Hz, and tones use the library's syntax (`ctcss:100.000`, `dcs:023`). Queries are the same as the CLI's, in a `q` parameter or a `query` field. Channel IDs can be shortened to a unique prefix.
- **Changes:** `PATCH` changes only the fields given, and `null` clears `tx_hz` and `location`. Changes to the `shared` library, organizations, systems, and plans need a `reason`.
- **Errors** are `{"error": {"code", "message"}}`: 400 for a request to fix, 401 without a valid token, 403 for everyday changes to the shared library, 404, 409 for a name in use, and 500 for anything else (the detail goes to the server's log).
- **The feed:** `GET /libraries/shared/feed?since=CURSOR` returns shared channels changed since a previous response's `cursor`. It's how a local copy will keep up with a hosted shared list.

## Development

```sh
make check      # go vet, golangci-lint, and go test
make web        # build the web interface into web/dist (needs Node.js)
make build      # build rflib, embedding web/dist when it's built
make web-check  # type-check the interface and run its unit tests
make e2e        # rebuild everything (replacing ./rflib) and run the browser smoke tests
```

The web interface lives in `web/`: React, TypeScript, Vite, Mantine, and TanStack Query. Its API types are generated from `internal/api/openapi.yaml` (`npm run gen`, run by every build), so an API change that breaks the interface fails its type check. For development, run `rflib serve` and `npm run dev` in `web/`; Vite serves the interface and passes `/api` through.

The CLI (`cmd/rflib`) parses flags and prints results, and the HTTP API (`internal/api`) decodes requests and encodes responses. The operations themselves live in `internal/app`, which both share. Errors made by a request are `domain.ErrInvalid` (`domain.Invalidf`), which the API reports as 400; other failures are 500. `internal/app` works on one library through the `store.Store` and `store.Library` interfaces (`internal/store`); `internal/store/sqlite` implements them. Any other backend must pass the contract tests in `internal/store/storetest`.

### Maintaining built-in data

Built-in lists and radio models come from outside sources: WSJT-X's and JS8Call's frequency lists, AMSAT, the NWS radiofax schedules, the FCC rules in the eCFR, and CHIRP's drivers. `tools/datacheck/sources.yaml` records each one's source and how it's checked, and every built-in list and model must appear there. The `Data check` workflow runs weekly and opens or updates one issue per dataset that differs from its source (label `data-check`). To run it locally:

```sh
uv run tools/chirp_model.py --json > /tmp/chirp.json   # CHIRP checks need this
go run ./tools/datacheck -chirp /tmp/chirp.json         # -only ID,ID for some datasets
```

Resolve a finding by fixing the data, or by recording an accepted difference or a fresh review in `sources.yaml`. In Claude Code, `/maintain-data` works through open findings. GitHub stops scheduled workflows in repositories with no activity for 60 days; re-enable it from the Actions tab, or run the same commands from cron with `GH_TOKEN` set and `-issues`.

## License

Copyright (C) 2026 Ryan Geyer.

rflib is free software: you can redistribute it and/or modify it under the terms of the GNU Affero General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version (AGPL-3.0-or-later); see [LICENSE](LICENSE). In short: anyone who distributes a modified rflib, or runs one as a service for others (such as a club's instance), must make its source available to those who receive or use it. The built-in channel lists and shared list data aren't covered by a separate data license yet.
