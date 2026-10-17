# Radios

A radio is one of your own radios: a model, the channels programmed into it in your order, and per-radio choices. Build each radio from your channels and the shared list, then export its programming file.

```sh
./rflib model list
./rflib radio add Truck --model yaesu-ftm150 --default-power medium
./rflib radio draft Truck --start
./rflib radio channels add Truck -q tag:mobile --sort freq
./rflib radio channels add Truck --id <channel-id>
./rflib radio channels list Truck
./rflib radio draft Truck --save
./rflib radio export Truck --output truck.csv
```

## Drafts

Changes to a radio's list (its channels, sections, and sources, and sync) go to its **draft**, which you save when you're happy with it, or discard. Nothing changes on the radio until then, so a sync or a big reorganization is safe to try.

```sh
./rflib radio draft Truck --start      # a copy of the list as it is now
./rflib radio draft Truck              # whether one is open, since when, and what it changes
./rflib radio draft Truck --save       # make it the radio's list
./rflib radio draft Truck --discard    # drop it
```

- **One draft per radio,** shared by the web and the CLI: starting another while one is open is refused, and every window and the CLI continue the one that's open.
- **The CLI needs a draft started** for any command that changes the list; it says how when there's none. The web opens one with your first change, and shows **Draft since …** with **Save draft** and **Discard draft**.
- **While a draft is open,** listing the radio's channels, sections, and sources shows the draft (the CLI says so on stderr; `--saved` shows the saved radio), and so does comparing with the radio's file.
- **The saved radio** is what exports, copies, and other radios' comparisons use. With a draft open, `radio export` says so on stderr, and the web asks whether to save the draft first.
- **Settings** (`radio update`, or **Edit radio…**) save at once, never in a draft. Changes to channels themselves, including retiring and relinking them, reach the saved radio and the draft alike.
- **A page that's out of date** (a draft started, saved, or discarded elsewhere since it loaded) has its next change refused, and reloads to show the radio as it is now. The radio page also reloads when you come back to it.
- **Sync would change N,** on the radio list and page, says that a sync of the saved radio would add or take off that many channels.

## Sections, channel order, and memory numbers

A radio's list is in **sections**, in order, and every memory belongs to one. A new radio has one, "Other", its **default section**, where channels added by hand go; each source gets a section of its own unless you point it at one.

```sh
./rflib radio sections add Truck "APRS & Winlink" --start 90 --size 10
./rflib radio sections add Truck GMRS --sort name
./rflib radio sections move Truck GMRS --to 1
./rflib radio sections set Truck GMRS --start 100     # or --start "" to follow the section before
./rflib radio sections list Truck                     # number, name, start, size, memories, order, channels
./rflib radio sections merge Truck "GMRS (from Shack)" --into GMRS   # its channels go to the end of GMRS
./rflib radio sections remove Truck GMRS              # its sources go too; its channels to the default section (--take-off: off the radio)
```

- **Start:** a section starts at `--start`, or right after the section before it. A start behind the sections before it is refused, as is anything that makes the list run past the model's memories.
- **Size:** `--size` reserves that many memories, so the next section without a start of its own begins after them. A section holding more spills over: the extra memories are numbered past its end, with a warning, and the Memories tab marks them.
- **Order:** a section's memories are in the order you place them (`manual`), or sorted by `name` or `freq`, whatever order they were added in. Placing a memory in a sorted section by hand makes it manual, in the order it had.
- **Default:** `--default` makes a section the default; the old default stops being it. The default section can't be removed, but it can be merged into another, which becomes the default.
- **Merging:** `radio sections merge` moves a section's channels to the end of another, in their order (or the other's sort), points its sources at the other, and removes it.
- **Moves always happen:** moving a section, merging, or moving memories into a section never fails for want of room: a section whose start the change leaves behind the memories before it loses its start and follows the section before it, and the command names it.

`radio channels add` appends to the end of a section (`--section NAME`, or the default section), by `--id` or by `--query`. Channels the radio can't receive are skipped and listed on stderr: those outside its model's bands (`--any-band` adds them anyway), and those in a mode it doesn't have, such as USB on an FM handheld (`--any-mode`). `channel list tunes:Truck` previews what fits. A channel in a mode the radio lacks that is on it anyway, added with `--any-mode` or before modes were checked, is listed with a warning but left out of the export. `radio channels list` says how each memory is programmed when it isn't an ordinary one: `rx only`, `tx tone only`, or `not programmed` and why (retired, a mode the radio lacks, or something the radio's file can't hold). A memory that isn't programmed stays in the list but takes no number (`-`), so the numbers are the radio's own once it's programmed. When a radio can't hold a channel's receive tone, such as split tones, the memory is programmed with its transmit tone alone, which is what opens a repeater. Channels already on the radio are skipped, so adding never reshuffles anything. Rearrange the list at any time:

```sh
./rflib radio channels move Truck --id <id> --before <other-id>   # or --after; into that channel's section
./rflib radio channels move Truck --id <id> --section GMRS --to 2 # or --top, --bottom; --section alone: its bottom
./rflib radio channels move Truck --id <id>,<id> --section GMRS   # several, to its bottom, in their order
./rflib radio channels remove Truck --id <id>    # or -q QUERY --yes, or --all --yes
```

Memory numbers follow the list from the model's first memory, section by section. Retiring a channel leaves it on its radios, crossed out and unprogrammed, until you take it off or relink it.

## Per-radio settings

`radio channels set` overrides a channel on one radio only. An empty value removes the override, so the channel's own setting applies again. `--power` and `--scan` can be set on many channels at once with `-q` (see [Changing many channels at once](channels.md#changing-many-channels-at-once)).

| Flag | Overrides |
|---|---|
| `--name` | The name on this radio (still fitted to its display) |
| `--power low\|medium\|high\|max` | The power on this radio |
| `--scan yes\|no\|default` | Whether this radio scans the channel. The channel's `--skip` is the default. |

`radio channels list Truck` shows every memory as it will be programmed: number, section, name on the radio and how it was fitted, power level, scan, frequency, duplex and offset, tone, and the library channel's name when the radio's differs. `*` marks a radio override, and `--fields` can add `plan_ch`, a plan's channel number. Tones read as repeater lists write them: `T131.8` sends a tone, `TSQL 131.8` also squelches on it, and `T131.8 R88.5` uses different ones; DCS codes read `D023N` or `D023I`. Warnings, such as a power cap or a frequency outside the radio's bands, go to stderr, and `radio export` repeats them.

Each radio has its own `--dedupe` (`none`, `index`, or `freq`) for duplicate names, and `--name-length` for a radio whose model doesn't know how long a name it shows, such as `chirp-generic` (0 uses the model's); see [Names on radio displays](names.md).

## Where radios transmit

A radio's memories transmit only where a ham or personal-radio user may: the US amateur bands, FRS and GMRS channels (plus GMRS repeater inputs), MURS, and CB. Any other channel is programmed receive-only, with a warning, whatever its service or receive-only setting says. A public safety repeater pair imported without `--service` can therefore never transmit on its input.

On CHIRP radios, receive-only means duplex `off`. The Yaesu FTM-150 and FTM-6000 have no per-memory transmit inhibit, so those memories are written as simplex, and the radios themselves refuse to transmit outside 2 m and 70 cm.

## Keeping radios up to date

A radio's **sources** are searches, or plans, that `radio sync` adds new matching channels from, each into its section.

```sh
./rflib radio draft HT --start                          # changes go to a draft (see Drafts)
./rflib radio sources add HT @local-rptrs --sort freq
./rflib radio sources add HT band:gmrs --sort freq --section GMRS
./rflib radio sources add HT band:noaa                  # a new section of its own, "band:noaa"
./rflib radio sections set HT band:noaa --start 150
./rflib radio sync HT --dry-run
./rflib radio sync HT
./rflib radio sources list HT    # number, section, sort, plan numbers, channels added, query
./rflib radio draft HT --save
```

- **Placement:** a source's new channels go at the end of its section, in its sort order; a sorted section keeps its own order. Several sources can feed one section.
- **Order and claims:** sources are numbered from 1. An earlier source claims a channel first, and `radio sources move HT 3 --to 1` changes the order.
- **Bands and modes:** like `radio channels add`, sync skips channels outside the radio's bands or in modes it lacks, reports how many, and takes `--any-band` and `--any-mode`.
- **Narrowing:** sync keeps a source's channels equal to its search. What a source added that its search no longer matches comes off, wherever it now is (`--dry-run` lists it first). Each memory remembers which source added it (the `source` column; **S2** on the Memories tab), independent of its section, so renaming, merging, and moving sections keep that. Channels added by hand never come off; `radio channels keep RADIO --id ...` (or **Keep** on the Memories tab) makes a source's channel count as added by hand. Note that `near:` never matches a channel without a location: narrow with `(near:... OR -has:location)` to keep those.
- **What sync leaves alone:** a channel already on the radio stays where it is, wherever that is. To keep a source's channels in order, sort its section.
- **Changing a source:** `radio sources set HT 2 --query ... --sort ... --section ...` changes a source; its channels already on the radio stay where they are.
- **Removing a source:** `radio sources remove HT 2` removes a source and leaves its channels on the radio. Add `--with-channels` to take them off too.
- **Plans:** a source can be a plan instead of a query, and then follows the plan; see [Plans](organizations-systems-plans.md#plans).

Sync warns when the result no longer fits the radio, such as a section running into the next one's start.

---

[Documentation index](README.md)
