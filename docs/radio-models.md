# Radio models

## Built-in models

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

`rflib model show NAME` prints a model's full facts, including allowed name characters, tunable bands, modes, and where the facts came from: CHIRP's driver source, or the radio's operating manual. `rflib model list` also lists your own models and the directory they're read from: `models` in rflib's settings folder, which is `~/.config/rflib` on Linux, `~/Library/Application Support/rflib` on macOS, and `%AppData%\rflib` on Windows (or `$XDG_CONFIG_HOME/rflib` anywhere, when that's set).

## Radios CHIRP supports but rflib doesn't

**The quick way:** use `chirp-generic` and tell rflib how long a name the radio shows.

```sh
./rflib radio add HT --model chirp-generic --name-length 7
```

Nothing else is checked: rflib doesn't know the radio's bands, modes, or memory count, so make sure its channels suit it. Export the file, then in CHIRP download your radio's image and import the file into it. CHIRP drops characters the radio can't show, and since power is left blank, it picks the radio's middle or highest power level.

**A model of your own** gets rflib's checks and per-memory power. `rflib model new` copies `chirp-generic` (or `--from` another model) into your models directory; edit it to match your radio, then check it:

```sh
./rflib model new my-ht            # prints the new file's path
./rflib model validate             # checks every model of yours
./rflib radio update HT --model my-ht
```

To fill it in from CHIRP itself, `tools/chirp_model.py` drafts a model from CHIRP's driver: memories, name length and characters, power levels with watts, bands, modes, and tone modes (`tone_modes`, and `cross_modes` for radios with CHIRP's Cross). A radio without Cross, such as the TYT TH-9800, can't hold different transmit and receive tones in one memory, so rflib programs those channels with the transmit tone alone and marks them; a model without `tone_modes` is assumed to take them all. It needs [uv](https://docs.astral.sh/uv/), which fetches CHIRP the first time:

```sh
uv run tools/chirp_model.py --list uv-5r                     # find the radio as CHIRP names it
uv run tools/chirp_model.py "Baofeng UV-5R" > baofeng-uv-5r.yaml
./rflib model install baofeng-uv-5r.yaml
```

Check the draft against the radio's manual or a CHIRP export of your radio: some drivers depend on the radio's region or firmware, which the draft can't see. Optionally tag power levels with an `intent` (low, medium, high, or max) to choose which level each power setting uses.

**A model file someone sends you,** such as one to try for a radio rflib doesn't have yet, or a fix to a built-in model:

```sh
./rflib model install my-ht.yaml             # check it, and add it to your models
./rflib model install my-ht.yaml --replace   # a newer copy of one you have
./rflib model validate                       # check every model of yours
./rflib model remove my-ht                   # take it out again
```

- **Installing** copies the file into your models folder, named after the model, so you can delete the one you downloaded. A model with a built-in model's name replaces the built-in.
- **Where each comes from:** `model list` says whether each model is built in, yours, or yours in place of a built-in. `radio show` and `radio export` say so for a radio whose model is yours, and the web interface marks installed models in its model lists.
- **A file that can't be used,** such as one with a mistake, is skipped with a warning, and everything else keeps working. Using the model says what's wrong with its file.
- **When a fix reaches a release:** once a built-in model says the same as the file replacing it, `model list` suggests `rflib model remove`, so you go back to the built-in and get its later fixes.

**Share it:** once a model works on your radio, [submit it](https://github.com/rgeyer/rflib/issues/new?template=3-radio-model.yml) with a sample CHIRP export, so others get it built in.

---

[Documentation index](README.md)
