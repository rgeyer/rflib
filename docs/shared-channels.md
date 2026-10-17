# Shared channels

The shared list holds channels shared with everyone: the built-in lists, and the club's own. Your radios use shared channels directly, by their IDs, and follow their corrections, such as a repeater's new tone. Searches for radios (`radio channels add -q`, and radio sources) look at your own channels and the whole shared list.

```sh
./rflib channel list in:shared band:noaa            # search the whole shared list
./rflib radio channels add HT -q 'band:noaa'         # shared channels go straight on a radio
./rflib channel update --id <id> --notes "home QTH"  # your notes, tags, short names on a shared channel
./rflib shared link --dry-run                       # which of your channels match shared ones
./rflib shared link                                 # ...replace them with the shared ones
./rflib channel copy --id <id>                      # a private copy you can edit
```

- **One list:** `channel list` (and the web's Channels page) shows the channels you use: your own, and shared channels you've annotated or put on a radio. `in:shared` searches the whole shared list, `in:all` both, and `in:mine` only your own; in the web interface, the Source filter does the same. Shared channels are marked as such. When the shared list has more matches than the channels you use, `channel list` says how many (on stderr), and the web interface offers to search there too.
- **What's yours on a shared channel:** tags, notes, and short names. Power and scan are set on each radio (`radio channels set`). Everything else belongs to the shared channel, and editing it is refused: make a private copy with `channel copy`, which takes the shared channel's place on your radios and stops following changes. The shared channel's tags and notes are kept beside yours, and searches match both.
- **Names:** a shared channel shows the shared name. Rename it on a radio (`radio channels set --name`), or give it short names. Converting one of your channels keeps a name of your own as a short name.
- **Converting:** `shared link` replaces a private channel with the shared one it matches on the air (the same frequencies, tones, and tone squelch, and the same service, or none; FRS and GMRS share frequencies, so the service keeps them apart). The shared channel takes its place on your radios, its tags, notes, and name become yours on the shared channel, and the private channel is retired, with the shared channel as its replacement. In the web interface, **Link to shared list** on the Channels page lists your matching channels and replaces the ones you pick.
- **Shared tags** come from a short list with agreed meanings: `rflib tag list`. None repeats a channel's fields (service, band, mode, or duplex), which search finds without a tag. Your own tags are free-form, and one with a shared tag's name means the same thing. Adding one of your own that repeats the channel's fields, such as `cb` on a CB channel, `ham` on an amateur one, or `repeater` on a repeater, is allowed but warns you that `service:cb` (and so on) already finds it.
- **The shared list is read-only** to everyday commands. Its maintainer changes it with `rflib shared import`, `update`, and `status`, each with a `--reason`. On a server with accounts, the maintainers are its admins.

## Status and changes

Every channel, shared or your own, is `active`, `off-air` (temporarily, such as for maintenance, with an expected-back date if known), or `retired` (gone for good, possibly replaced by another channel). Channels are never deleted: one you no longer want is retired. A small change, such as a new tone, is a correction to the same channel; a repeater that becomes a different system is a new channel, and the old one is retired and points to it.

- **On your radios:** an off-air channel stays programmed, so it needs no reprogramming when it's back, but it isn't scanned. A retired channel isn't programmed at all: it stays on the radio's list, crossed out, but takes no memory number. If it's ever back in service, it's programmed again.
- **Sources take their search as it is:** a retired channel a source's search matches is added, crossed out, and stays while the search matches it. Add `-status:retired` to a source to leave retired channels out.
- **The channel list hides retired channels** unless the search says `status:` (such as `status:retired`); `channel list --retired`, or **Show retired** on the Channels page, shows them. It says how many it left out. Other searches, such as radio sources, never hide anything.
- **Your own channels:** `rflib channel status` sets their status, by `--id` or `--query`, and a retired one can name its replacement, one of yours or a shared channel (`--replaced-by`). `rflib channel relink` puts a retired channel's replacement in its place on your radios, keeping their settings, for your own channels and shared ones. A shared channel's status is the shared list's.
- **`rflib shared changes`** lists the shared channels you use (on your radios, or with your notes and tags) that changed since you last looked: what changed, why, and the radios they're on, which need programming again. `rflib shared ack --all` marks them seen. `rflib shared relink --all` moves retired channels' places on your radios, and your notes and tags, over to their replacements.
- **History:** every change to a channel is kept, with when and why: a shared channel's corrections, and every change to your own channels (including power, scan, and short names). `rflib channel history --id <id>`, or **History** in the channel editor. Your notes on a shared channel (tags, notes, and short names) keep a history too, even once removed, so a radio revision can show them as they were.
- **Searching:** `status:off-air` finds channels that are down.

```sh
./rflib channel status --id <id> --set retired --note "the club moved it" --replaced-by <new-id>
./rflib channel relink --id <id>
./rflib shared status --id <id> --set off-air --note "antenna work" --back 2026-12-01 --reason "owner reported"
./rflib shared status --id <id> --set retired --replaced-by <new-id> --reason "moved to Diablo Peak"
./rflib shared update --id <id> --tx-tone ctcss:131.8 --reason "new tone"
./rflib shared import csv club-repeaters.csv --reason "club list, October"
```

---

[Documentation index](README.md)
