# Built-in channel lists

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
| `us-cb` | CB 1-40 in AM, and again in LSB (`CB 1 LSB`) and USB (`CB 1 USB`) for SSB radios |
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

Built-in lists fill the shared list; your library takes channels from there (see [Shared channels](shared-channels.md)). A new database's shared list starts with every list (`rflib init --no-seed` starts it empty). Upgrading rflib never changes the shared list: when a newer version changes a list, `seed list` shows the changes pending, and `seed update` applies them, as the maintainer chooses. An update adds the list's new channels, applies what the list changed since it was last applied, and retires channels it dropped. A field the maintainer corrected keeps the correction, and the update reports it. Each change is in the channel's history, with the list as its reason. The lists carry only shared tags, such as `calling`; the old `gmrs`, `noaa`, and similar tags repeated the service and band. In the web interface, **Built-in lists** is under the Shared list in the navigation.

---

[Documentation index](README.md)
