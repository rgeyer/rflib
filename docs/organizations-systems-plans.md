# Organizations, systems, and plans

The shared list also records who runs its repeaters, how they're linked, and the communications plans that use them. Everyone reads them; changes are the maintainer's: each takes a `--reason`, and every change is kept in a history (`rflib org|system|plan history NAME`).

## Network nodes

A channel can carry its nodes on linking networks: AllStar, EchoLink, IRLP, and WIRES-X. Search them with `allstar:43761`, `echolink:`, `irlp:`, and `wires-x:`, or `has:network`. The web interface links each node to its network's own pages; rflib fetches nothing from the networks.

```sh
./rflib channel update --id <id> --networks allstar:43761,echolink:12345
./rflib shared update --id <id> --networks allstar:43761 --reason "node numbers"
```

## Organizations

Organizations are who runs or supports repeaters and systems: clubs, emergency services, agencies, and individuals. They're plain records, attached to shared channels and systems as an **operator** or a **partner** (such as a site's co-sponsor). `org:NAME` finds the channels an organization operates.

```sh
./rflib org add SBARC --callsign W6SBA --website https://www.sbarc.org --reason "pilot"
./rflib shared update --id <id> --operators SBARC --partners GRONK --reason "who runs it"
./rflib org show SBARC      # its channels and systems
./rflib org list
```

An organization attached to anything can't be deleted.

## Systems

A system is a set of interconnected channels, such as a club's linked repeaters, as its operators intend it: not its live connections. Members are shared channels, linked **full-time** (usually linked) or **on demand** (may be linked), with a note on how. A system can have nodes of its own, such as a hub's AllStar node, and its members can belong to different organizations. `system:NAME` finds a system's channels.

```sh
./rflib system add "SBARC Hub" --link allstar --networks allstar:43763 --operators SBARC --reason "pilot"
./rflib system members add "SBARC Hub" --id <id>,<id> --reason "pilot"
./rflib system members add "SBARC Hub" --id <id> --on-demand --notes "microwave to the hub" --reason "pilot"
./rflib system show "SBARC Hub"
./rflib channel list 'system:"SBARC Hub"'
```

The map draws lines between a system's members, dashed for members linked on demand.

## Plans

A plan is a communications plan: an ordered selection of shared channels, each with its name and assignment in the plan, laid out as ICS 205 lists them. An event is a plan with dates. A plan is `draft`, `published`, or `archived`, and is never deleted.

```sh
./rflib plan add "Fair 2026" --starts 2026-07-15 --ends 2026-07-26 --reason "county fair"
./rflib plan entries add "Fair 2026" --id <id> --name "FAIR TAC1" --assignment "Tactical" --reason "plan"
./rflib plan entries add "Fair 2026" -q 'system:"SBARC Hub"' --section-start 10 --assignment "Wide area" --reason "plan"
./rflib plan channels "Fair 2026"     # Ch #, name, assignment, frequencies, tones, mode, remarks
./rflib plan show "Fair 2026"         # the entries, numbered for entries move, set, and remove
./rflib plan update "Fair 2026" --status published --reason "ready"
```

- **Entries** are shared channels, renamed for the plan if you like (147.420 simplex as "FAIR TAC1", so plans rarely need new shared channels), or **query sections**, which bring in every shared channel a query matches, in name or frequency order, leaving out channels the plan lists elsewhere.
- **Numbers:** a plan numbers its channels from 1 (ICS 205's Ch #); `--section-start N` starts an entry at channel N.

**On a radio**, a plan is a source feeding a section: `rflib radio sections add HT Fair --start 50`, then `rflib radio sources add HT --plan "Fair 2026" --section Fair --plan-numbers match`. Its channels follow the plan:

- `radio sync` puts the plan's channels in its section in the plan's order, with the plan's names.
- **Plan numbers:** `--plan-numbers compact` (the default) numbers them in order without gaps; `match` keeps the plan's numbers, gaps and all, from the section's first memory: plan channel 10 becomes memory 59 when the section starts at 50, so a dedicated radio's numbers match the document. Either way, each memory shows its plan Ch #.
- Plans claim their channels before query sources; a plan's channel already elsewhere on the radio stays where it is.
- Channels the plan drops come off, and removing the source takes all of its channels off.
- An archived plan's channels stay as they are.

---

[Documentation index](README.md)
