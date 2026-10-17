# Search

`channel list` takes a query, much like a Gmail search. Every command that selects channels (`export`, `names`, `check`, and `radio channels add`) takes the same query with `--query` or `-q`.

```sh
./rflib channel list service:none freq:144..148 is:repeater
./rflib channel list 'tag:ham (near:34.64,-120.46,40 OR -has:location)'
./rflib export chirp -q 'band:2m OR band:70cm' --output dual.csv
```

- **Combining:** terms are `field:value`, and a space between them means AND. `OR` binds tighter than AND, so `a b OR c` means `a AND (b OR c)`.
- **Negating:** a leading `-` (or `NOT`) negates a term or a parenthesized group. When a negation comes first among the arguments, put `--` before it so it isn't read as a flag: `channel list -- -tag:hf`. Quoting a whole query, or passing it with `-q`, avoids this.
- **Grouping and quoting:** parentheses group terms, and quotes keep spaces in a value: `name:"Lompoc PD"`.
- **Bare words** match names, short names, and notes, ignoring case.

| Field | Values |
|---|---|
| `name:`, `notes:` | Text the name (or a short name), or the notes, contains |
| `service:` | A service, or `none` |
| `tag:` | A tag |
| `freq:`, `rx:`, `tx:` | MHz: `146.52`, a range `144..148` (either end optional), or `>=420`, `<30`. `freq:` matches either frequency. |
| `band:` | `160m` through `23cm`, `air`, `marine`, `cb`, `frs`, `gmrs`, `murs`, `noaa` (by receive frequency) |
| `duplex:` | `+`, `-`, `split`, `simplex`, or `off` (no transmit frequency) |
| `tone:`, `txtone:`, `rxtone:` | `131.8`, `dcs:023`, `ctcss`, `dcs`, or `none`, as programmed: the receive tone counts only with tone squelch. `tone:` matches either direction; `tone:none` means neither has one. |
| `mode:` | `FM`, `NFM`, `AM`, `USB`, `LSB`, or `CW` |
| `power:` | `low`, `medium`, `high`, `max`, or `none` |
| `squelch:`, `skip:` | `yes` or `no` |
| `is:` | `repeater` (transmit differs from receive), `simplex`, `rxonly`, `transmit` (a radio export would let it transmit; see [Where radios transmit](radios.md#where-radios-transmit)), `skip` |
| `has:` | `location`, `service`, `tag`, `tone`, `power`, `notes`, `shortname`, `radio`, `network`, `org` |
| `near:` | `LAT,LON,KM` or `GRID,KM`, see below |
| `radio:` | A radio's name, `any`, or `none` |
| `tunes:` | A radio or model name: channels in its bands and in a mode it has |
| `created:`, `updated:` | `YYYY-MM-DD`, with comparisons and ranges like `freq:` |
| `id:` | A channel ID |
| `status:` | `active`, `off-air`, or `retired` (see [Status and changes](shared-channels.md#status-and-changes)) |
| `in:` | `use` (the default): your own channels and the shared channels you annotate or have on a radio; `mine`: only your own; `shared`: the whole shared list; `all`: both. Only at the top level of a query. Radio sources and adding to a radio search your own channels and the whole shared list unless a query says otherwise. |
| `org:` | An organization's name or ID: channels it operates |
| `system:` | A system's name or ID: its members |
| `allstar:`, `echolink:`, `irlp:`, `wires-x:` | A node number on that network |

**Location.** `near:` matches when a channel's location is within the distance plus the channel's own `--radius-km`, so a repeater whose coverage reaches you counts. A grid square searches from its center: `near:CM94sq,40`. Channels without a location never match `near:`. To keep them, say so: `near:34.64,-120.46,40 OR -has:location`.

**Shorthand flags.** `--tag`, `--service`, and `--lat`/`--lon`/`--radius-km` also work, and are added to the query.

## Saved searches

Save a query by name and use it anywhere as `@name`: in `channel list`, in other searches, and in radio sources.

```sh
./rflib search add local-rptrs '(band:2m OR band:70cm) is:repeater near:34.64,-120.46,50'
./rflib channel list @local-rptrs -tag:hf
./rflib search list          # each search, how many channels it matches, and its query
./rflib search show local-rptrs
./rflib search update local-rptrs --query 'band:2m is:repeater'
```

A search can't use itself, directly or through others. A search in use by another search or a radio can't be renamed or deleted; `search show` lists what uses it.

---

[Documentation index](README.md)
