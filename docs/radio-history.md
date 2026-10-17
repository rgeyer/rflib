# Revisions, copies, and comparing

## Revisions and programming

A **revision** is the saved radio as it was at one time: its settings, sections, sources, and memories, each memory with the revision of its channel then. An export saves one when the radio changed since its last revision, and counts as programming the radio; you can also save one by hand.

```sh
./rflib radio export Truck --output truck.csv --reason "before the fair"   # saves a revision if Truck changed
./rflib radio revisions list Truck
./rflib radio revisions save Truck --reason "new club repeaters"
./rflib radio revisions compare Truck 3     # what changed since revision 3
./rflib radio revisions restore Truck 3     # put revision 3 into Truck's draft
```

- **Needs programming:** a radio that differs from its last exported revision, including channels on it that changed since (a corrected tone, say), needs programming again. `rflib radio list` and `show`, the radio list, and the radio page say so. A revision saved by hand doesn't count as programmed.
- **Export reasons:** the CLI's export saves its revision with the day and "saved automatically by CLI export" unless `--reason` says otherwise; the web asks why before an export that saves one. Exporting a radio that hasn't changed saves no revision.
- **Compare:** a revision against the radio as it is now (its draft, if one is open): memories added, removed, moved to another section, renumbered, or with other settings, channels whose own values changed, and sections and sources added or removed. On the web, pick a revision on the **Revisions** tab.
- **Restore:** a revision's sections, sources, and memories go into the radio's draft, to review and save or discard (see [Drafts](radios.md#drafts)). Settings stay as they are, and channels are as they are now, since they belong to the library: one retired since comes back crossed out. With a draft open, `--replace-draft` (or confirming on the web) replaces it.

## Copying a radio

`radio copy` makes a new radio from another as it is now, for another model: its sections in order (with their starts, sizes, and orders), its sources, its channels in their sections and order, and its overrides. Channels the new model can't use, outside its bands or in a mode it lacks, are left out and listed. Settings you don't give come from the original. From then on the two are independent.

```sh
./rflib radio copy Shack Mobile --model tyt-th9800
./rflib radio compare Mobile --radio Shack                 # by channel: on both, only on Shack, only on Mobile
./rflib radio compare Mobile --radio Shack --add all       # or --add ID,ID; --dry-run to check first
```

Comparing two radios pairs them by channel. `--add` brings what the other radio has onto this one, in the other's order, each into this radio's section of the same name, with the other radio's name, power, and scan for it. A section this radio lacks is made as the other radio has it (start, size, and order), at the end, after this radio's sections, without its start if that doesn't fit there; new channels that would push one of this radio's sections into a later section's start go to a section of their own at the end. Nothing already on the radio moves, and compare says what it did; drag the new sections into place afterwards; channels this radio's model can't use are listed but not added. It works one way, onto the radio you name first; compare the other way to go back.

## Comparing with the radio

`radio compare` compares a radio with a file of what it holds now, as its programming software or CHIRP saved it. Starting a radio you already programmed by hand is the case where it's still empty in rflib.

```sh
./rflib radio add Mobile --model yaesu-ftm150
./rflib radio compare Mobile mobile.csv                       # report only
./rflib radio compare Mobile mobile.csv --use-exact --keep all --dry-run
./rflib radio compare Mobile mobile.csv --use-exact --keep all
```

Each memory is **same**, **changed** (rflib has it there, but the radio's differs), **radio** (only on the radio), or **rflib** (only in rflib; the next export programs it). Memories pair by what they program, frequencies, tones, and mode; then as changed when only the receive tone or tone squelch differs (a repeater's tone set to transmit only is common, and works); then by memory number when the receive frequency or name is the same. A memory rflib's export leaves out, such as one with tones the radio's format can't hold, is still compared, with the reason. In the web interface, the values that differ are highlighted. For each memory only on the radio, compare lists channels it might be, from your channels and the shared list: an exact match, the same frequencies with other tones or mode, or the same receive frequency, with a matching name ranked higher. Each says what differs from the memory, such as `tx tone` or `name`, and in the web interface the channel you choose shows the radio's value and rflib's for each. Power isn't compared: files name power levels their own way.

- `--use 12=ID` puts that channel on the radio for memory 12; `--use-exact` does so with the best exact match for every memory that has one.
- `--keep 12,14` (or `all`) keeps memories as programmed, each as a private channel.
- `--take 3` (or `all`) gives rflib a changed memory's values: a private channel changes; a shared one is copied first; a different name or scan setting becomes this radio's own.

**First import:** when rflib already lays the radio out its own way (with sources, say) and you only want to be sure nothing on the radio gets lost, add `--ignore-numbers` (in the web interface, **Ignore memory numbers**). Memories then pair by what they program wherever they are, or as changed by name and receive frequency together; used and kept memories go at the end of the list, and rflib's order wins from then on.

Used and kept memories keep their numbers where rflib has nothing there, starting a section when needed; where rflib uses the number, they go at the end of the list, and compare says so. Undecided memories stay as rflib has them, so the next export removes a memory only on the radio. A channel put on the radio this way that a source also matches isn't added twice.

---

[Documentation index](README.md)
