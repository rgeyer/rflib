# Known limitations

rflib is in public preview. These are known; there's no need to report them, though a [feature request](https://github.com/rgeyer/rflib/issues/new?template=4-feature.yml) saying what one is costing you helps decide what comes first.

## Radios and channels

- **Analog only.** Channels are FM, NFM, AM, USB, LSB, or CW. Digital modes, such as DMR, D-STAR, P25, and System Fusion, aren't supported yet; importing a file with them reports those memories and skips them.
- **US rules.** The built-in lists, band plans, service power limits, and where a radio may transmit all follow US (FCC) rules.
- **Few radios are built in.** Nine models ship with rflib (see [Radio models](radio-models.md)). Any other radio CHIRP supports works through `chirp-generic`, which checks nothing about the radio: not its bands, modes, memory count, or power levels. A [radio request](https://github.com/rgeyer/rflib/issues/new?template=2-radio-request.yml) gets a model started.
- **Yaesu FTM-150:** tone squelch isn't mapped to ADMS-17's spelling yet, so a tone squelch channel fails the export (or is left out with `--skip-invalid`).
- **Split tones on some radios.** Radios that hold one tone per memory, such as the Yaesu FTM-150 and FTM-6000 and the TYT TH-9800, can't hold different transmit and receive tones. rflib programs those channels with the transmit tone alone, which still opens the repeater, and marks them.
- **Programming software isn't driven directly.** rflib writes a file; you load it into the radio with CHIRP or the manufacturer's software.

## Hosting and accounts

- **No email.** rflib sends none, so a forgotten password is reset by an admin (`rflib user passwd`).
- **No scripting a hosted server.** On a server with accounts, there are no personal API tokens yet, and the command line works only on a database on the same machine.
- **Members can't propose changes to the shared list.** Corrections and new shared channels are made by an admin for now.
- **No HTTPS of its own.** Off your own computer, rflib needs a proxy that serves HTTPS (see [Accounts and hosting](accounts.md)).

## Other

- **The map** needs an internet connection for its background (OpenStreetMap). Everything else works offline.
- **The binaries aren't signed,** so Windows and macOS warn the first time you run rflib (see [Install](../README.md#install)).
- **Shared channels are created from files.** An admin can correct a shared channel in the web interface, but adds new ones with `rflib shared import`.

---

[Documentation index](README.md)
