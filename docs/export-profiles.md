# Export profiles

Some radios are programmed with the manufacturer's software rather than CHIRP. An export profile describes such a tool's CSV memory format, and the same profile reads that format back in, so the files you already have become a starting library.

```sh
./rflib profile list
./rflib import profile yaesu-ftm150 "FTM-150 Software Programming.csv"
./rflib export profile yaesu-ftm150 --tag mobile --sort freq --output ftm150.csv
```

## Yaesu FTM-150 and FTM-6000

| Profile | Software | Names | Modes |
|---|---|---|---|
| `yaesu-ftm150` | ADMS-17 | 16 characters | FM, NFM, AM |
| `yaesu-ftm6000` | ADMS-13 | 6 characters | FM, NFM, AM |

In ADMS, open the *Memories* tab and use *File > Import* on the exported file. As ADMS's own exports do, the file holds all 999 memories: your channels first, then empty ones.

- **Power:** exporting a radio writes its level (`HIGH`, `MID`, `LOW`; see [Power](power.md)). Exporting channels directly maps their power intent, and channels without one get `HIGH`.
- **Scan:** skipped channels are written with scan `NO`.
- **Narrow:** NFM channels are written as FM with *Narrow* on.
- **Repeaters:** written as `+RPT`/`-RPT` with the offset, and cross-band as `-/+`. The transmit frequency is always written as well.
- **Receive only:** these radios have no per-memory transmit inhibit, so receive-only channels are written as simplex. Both radios only transmit on 2 m and 70 cm.
- **Step:** the coarsest of 25, 5, 6.25, or 12.5 kHz that the frequency fits.
- **Names:** fitted to 16 (FTM-150) or 6 (FTM-6000) characters as described in [Names on radio displays](names.md).
- **Tone modes:** `OFF`, `TONE` (encode), and `DCS` as confirmed in ADMS exports. On the FTM-6000, tone squelch is written `TONE SQL`, as the radio shows it; on the FTM-150 it isn't mapped yet and fails the export (or is left out with `--skip-invalid`). Neither radio holds different transmit and receive CTCSS tones in one memory, so a radio's export programs split-tone channels with their transmit tone alone (`export profile` of channels directly still refuses them). Override the profile, as below, to change these.

Importing reads every non-empty memory. Unnamed memories are named after their frequency, and unused tone columns are ignored.

## Writing your own profile

Profiles are YAML. Start from a built-in:

```sh
./rflib profile show yaesu-ftm150 > my-radio.yaml
./rflib profile validate my-radio.yaml
./rflib export profile my-radio.yaml
```

To use a profile by name, install it with `rflib profile install FILE`, which copies it into the directory `rflib profile list` prints: `profiles` in rflib's settings folder (see [Models](radio-models.md#built-in-models)). A profile with a built-in's name replaces the built-in, which is how to fix a format locally. `profile install --replace`, `profile remove`, and `profile validate` with no file work as they do for models (see [Radios CHIRP supports but rflib doesn't](radio-models.md#radios-chirp-supports-but-rflib-doesnt)).

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

---

[Documentation index](README.md)
