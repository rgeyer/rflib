# Names on radio displays

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

---

[Documentation index](README.md)
