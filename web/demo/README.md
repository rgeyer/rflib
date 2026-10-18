# The demo

Two scripts make rflib's demo: a library with two radios built from the shared list, the README's screenshots and recording, and the public demo account on a hosted instance.

## `populate.mjs`: the demo library

Fills an empty library through the API, as a person would: one channel of the library's own (a Field Day talk-in), and two radios built by searches.

- **HT** (`baofeng-uv5rm`, high power): calling channels, repeaters within 45 km of Santa Maria, CA, and NOAA weather, each in a section of its own.
- **Truck** (`yaesu-ftm150`, medium power): those repeaters, nearby Winlink gateways, and weather.

Both are exported once, so a later correction to the shared list shows them needing programming. The searches expect a shared list with Central Coast California repeaters, as the hosted instance's is.

```sh
node demo/populate.mjs --url http://127.0.0.1:8080                                 # without accounts
node demo/populate.mjs --url https://rflib.example.org --user demo --password '...'   # a member's library
```

It refuses a library that already has radios. To start over on a server with accounts, remove the account (`rflib user remove demo --yes`) and add it again.

## `media.mjs`: screenshots and the recording

Builds a database from a shared list export, serves it, populates it, and drives the web interface with Playwright:

```sh
make web
go build -buildvcs=false -ldflags "-X github.com/ryan/rflib/internal/buildinfo.Version=v0.1.0" -o rflib ./cmd/rflib
rflib --db <a library with the shared list> shared export json --output /tmp/shared.json
cd web && node demo/media.mjs --shared /tmp/shared.json --out ../docs/media
```

The version stamp, and `-buildvcs=false`, make the sidebar read as a release does. It writes:

| File | Shows |
|---|---|
| `memories.png` | The HT's memories, in sections, as they'll be programmed |
| `draft.png` | A draft's changes marked: a repeater added, one taken off, and scanning turned off on another |
| `compare.png` | A comparison with the radio's file: a tone and a power level changed on the radio |
| `map.png` | The shared list's repeaters on the map, clustered |
| `demo.webm` | The recording: a repeater's tone is corrected once, both radios show they need programming, and the HT is exported |

Then, with ffmpeg, make the README's GIF from the recording (about 3.5 MB: 8 frames a second, 64 colors, which suits the interface's flat colors, less the first second, while the page loads), and the map a JPEG (its tiles make a large PNG). Neither `demo.webm` nor `map.png` is committed:

```sh
cd docs/media
ffmpeg -y -ss 1.2 -i demo.webm -vf "fps=8,scale=960:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=64:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" demo.gif
ffmpeg -y -i map.png -q:v 3 map.jpg
rm demo.webm map.png
```
