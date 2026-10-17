# rflib

By Ryan Geyer, W6FAM.

`rflib` keeps your analog radio channels in one library and programs your radios from it. You collect channels once, your own and those on a shared list of repeaters and common frequencies, then build each radio from them and export its programming file for CHIRP or the manufacturer's software (Yaesu ADMS). When a shared channel changes, such as a repeater's new tone, the radios that use it show they need programming again.

It's one program, `rflib`, with a web interface and a command line, on a single SQLite file. Run it on your own computer, or host it for a club, with an account for each person.

**rflib is in public preview.** It works, and its author programs real radios with it, but expect rough edges, and see the [known limitations](docs/limitations.md) before you start. Feedback is welcome: see [Questions, problems, and radios](#questions-problems-and-radios).

## Install

Download the archive for your system from the [latest release](https://github.com/rgeyer/rflib/releases/latest) and unpack it. It holds `rflib` (`rflib.exe` on Windows), this README, the [documentation](docs/README.md), and the license. There's no installer: put `rflib` wherever you like, such as a folder on your `PATH`.

| System | Archive |
|---|---|
| Windows (most PCs) | `windows-amd64.zip` |
| Windows on ARM (such as Snapdragon laptops) | `windows-arm64.zip` |
| macOS on Apple silicon (M1 and later) | `macos-arm64.tar.gz` |
| macOS on Intel | `macos-amd64.tar.gz` |
| Linux on x86-64 | `linux-amd64.tar.gz` |
| Linux on 64-bit ARM (such as a Raspberry Pi 4 or 5 with a 64-bit OS) | `linux-arm64.tar.gz` |
| Linux on 32-bit ARM (older Raspberry Pis, or a 32-bit OS) | `linux-armv7.tar.gz` |

`SHA256SUMS` in the release lists each archive's checksum: `sha256sum -c SHA256SUMS --ignore-missing` checks the one you downloaded (on macOS, `shasum -a 256 -c`; on Windows, compare with `Get-FileHash` in PowerShell).

**The binaries aren't signed,** so Windows and macOS warn the first time you run one:

- **Windows:** before unpacking, right-click the zip, choose **Properties**, tick **Unblock**, and choose **OK** (or run `Unblock-File` on it in PowerShell). If Windows still shows "Windows protected your PC", choose **More info**, then **Run anyway**.
- **macOS:** after unpacking, run `xattr -d com.apple.quarantine rflib` in Terminal, in the folder you unpacked it into. Otherwise, when macOS says it can't verify the developer, open **System Settings > Privacy & Security** and choose **Open Anyway** for `rflib`.
- **Linux:** nothing to do; the archive keeps `rflib` executable.

## Quick start

`rflib` runs from a terminal: PowerShell on Windows, Terminal on macOS. In the folder you unpacked it into, make your library, then start the web interface:

```sh
./rflib init     # on Windows: .\rflib.exe init
./rflib serve    # then open http://127.0.0.1:8080/ ; Ctrl+C stops it
```

Your library starts with a shared list of common channels: FRS, GMRS, MURS, CB, NOAA weather, amateur calling frequencies, and more. Your radios use those directly, beside channels of your own, such as your club's repeater.

### In the web interface

1. **Add a radio:** on **Radios**, choose **Add radio**, give it a name, and pick its model. For a radio CHIRP supports that isn't listed, choose **chirp-generic** and set how many characters its display shows.
2. **Put shared channels on it:** on **Channels**, set **Source** to **the shared list**, and search, for example `band:noaa`, or `tag:calling band:2m`. Select the channels you want, and choose **Add to radio**.
3. **Add one of your own:** on **Channels**, choose **New channel**, fill in its frequencies and tones, and add it to the radio the same way.
4. **Check it:** open the radio. The **Memories** tab shows each memory as it will be programmed, with any warnings. Your changes are in a draft until you choose **Save draft**.
5. **Program the radio:** choose **Export** and save the file. In CHIRP, download the radio's memory from the radio, choose **File > Import** with the exported file, then upload to the radio. For a Yaesu radio, import the file in ADMS on the **Memories** tab.

### On the command line

The same, as commands:

```sh
./rflib radio add HT --model baofeng-uv5rm      # rflib model list shows the models
./rflib radio draft HT --start                  # changes go to a draft until you save it
./rflib radio channels add HT -q 'tag:calling (band:2m OR band:70cm) mode:FM'
./rflib radio channels add HT -q band:noaa
./rflib channel add --name "Club Repeater" --rx 146.940 --tx 146.340 --tx-tone ctcss:100.0 --service amateur \
  | ./rflib radio channels add HT --id -       # a channel of your own, straight onto the radio
./rflib radio channels list HT                  # each memory as it will be programmed
./rflib radio draft HT --save
./rflib radio export HT --output ht.csv         # then load ht.csv into CHIRP, as above
```

When a shared channel you use changes, such as a repeater's new tone, the radio shows **Needs programming**: export it again and load it into the radio.

`rflib version` says which rflib you have; include it when you report a problem.

## Learn more

The [documentation](docs/README.md) covers everything else: searching, radios' sections and sources, radio models and model files, the shared list, organizations and plans, importing from CHIRP and RadioReference, hosting with accounts, and the HTTP API.

## Questions, problems, and radios

- **Questions,** and ideas that aren't ready to be a request: [Discussions](https://github.com/rgeyer/rflib/discussions).
- **Something doesn't work:** a [bug report](https://github.com/rgeyer/rflib/issues/new?template=1-bug.yml).
- **A radio rflib doesn't have:** a [radio request](https://github.com/rgeyer/rflib/issues/new?template=2-radio-request.yml), with a sample export from its programming software. You may be sent a model file to try; `rflib model install FILE` adds it (see [Model files](docs/radio-models.md#radios-chirp-supports-but-rflib-doesnt)).
- **A model you've made:** a [radio model submission](https://github.com/rgeyer/rflib/issues/new?template=3-radio-model.yml).
- **Anything else rflib could do:** a [feature request](https://github.com/rgeyer/rflib/issues/new?template=4-feature.yml).

## License

Copyright (C) 2026 Ryan Geyer, W6FAM.

rflib is free software: you can redistribute it and/or modify it under the terms of the GNU Affero General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version (AGPL-3.0-or-later); see [LICENSE](LICENSE). In short: anyone who distributes a modified rflib, or runs one as a service for others (such as a club's instance), must make its source available to those who receive or use it. The web interface links to the source of the rflib it's running, on the sign-in page and in the sidebar; someone serving a modified rflib points that link at their own source with `rflib serve --source-url`. The built-in channel lists and shared list data aren't covered by a separate data license yet.
