# Power

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

**Power in imported files.** Imports read power text as intents: HIGH, MID, LOW, and similar become `high`, `medium`, and `low`, and anything else, such as `50W`, is dropped. Watts in a RadioReference or CHIRP file describe the source's radio, not yours.

---

[Documentation index](README.md)
