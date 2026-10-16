# /// script
# requires-python = ">=3.10"
# dependencies = ["chirp @ git+https://github.com/kk7ds/chirp"]
# ///
"""Draft an rflib radio model from a CHIRP driver.

    uv run tools/chirp_model.py --list uv-5r      # find the CHIRP radio
    uv run tools/chirp_model.py "Baofeng UV-5R"   # print a model file
    uv run tools/chirp_model.py --json            # every radio, for tools/datacheck

The draft holds what CHIRP's driver declares: memories, name limits, power
levels, bands, and modes. Check it against the radio's manual or a CHIRP
export, set each power level's intent if you want, save it in the user
models directory (rflib model list prints it), and check it with
rflib model validate.
"""

import argparse
import datetime
import json
import logging
import re
import string
import sys

# rflib's modes; CHIRP modes outside these are left out of the draft.
RFLIB_MODES = ["FM", "NFM", "AM", "USB", "LSB", "CW"]

# rflib's base name character sets, smallest first.
CHARSETS = [
    ("upper_numeric", set(string.ascii_uppercase + string.digits + " ")),
    ("alnum", set(string.ascii_letters + string.digits + " ")),
    ("ascii", {chr(c) for c in range(0x20, 0x7F)}),
]


def drivers():
    logging.disable(logging.CRITICAL)
    from chirp import directory

    directory.import_drivers()
    return directory.DRV_TO_RADIO


def label(cls):
    """The radio as CHIRP's radio list shows it: vendor, model, variant."""
    name = f"{cls.VENDOR} {cls.MODEL}"
    variant = getattr(cls, "VARIANT", "")
    return f"{name} ({variant})" if variant else name


def slug(text):
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")


def charset(valid):
    """The largest base set the radio fully supports, plus its extra
    characters."""
    valid = set(valid)
    base_name, base = CHARSETS[0]
    for name, chars in CHARSETS:
        if chars <= valid:
            base_name, base = name, chars
    extra = "".join(sorted(c for c in valid - base if 0x20 <= ord(c) < 0x7F))
    return base_name, extra


def mhz(hz):
    return float(f"{hz / 1e6:.6f}")


def features(cls):
    """What the driver declares, in rflib's terms."""
    from chirp import chirp_common

    rf = cls(None).get_features()
    base, extra = charset(rf.valid_characters)
    names = {"max_length": rf.valid_name_length, "charset": base}
    if extra:
        names["extra"] = extra
    # CHIRP keeps power in dBm (float(p); int(p) truncates); rflib lists
    # levels from lowest to highest.
    power = []
    for p in rf.valid_power_levels:
        watts = chirp_common.dBm_to_watts(float(p))
        power.append({"label": str(p).strip() or f"{watts:g}W", "watts": watts})
    power.sort(key=lambda level: level["watts"])
    return {
        "driver": f"{cls.__module__}.{cls.__name__}",
        "memories": {"first": rf.memory_bounds[0], "last": rf.memory_bounds[1]},
        "names": names,
        "power": power,
        # Some drivers declare placeholder bands, such as (0, 0).
        "bands_mhz": [[mhz(lo), mhz(hi)] for lo, hi in rf.valid_bands if 0 < lo <= hi],
        "modes": [m for m in RFLIB_MODES if m in rf.valid_modes],
        "other_modes": sorted(set(rf.valid_modes) - set(RFLIB_MODES) - {""}),
        # The tone modes a memory can use, and with Cross, its cross modes.
        "tone_modes": list(rf.valid_tmodes),
        "cross_modes": list(rf.valid_cross_modes) if "Cross" in rf.valid_tmodes else [],
    }


def version():
    import chirp

    return getattr(chirp, "CHIRP_VERSION", "unknown")


def draft(cls):
    f = features(cls)
    lines = [
        f"name: {slug(label(cls))}",
        f"description: {json.dumps(label(cls))}",
        "format: chirp",
        f"chirp_model: {json.dumps(label(cls))}",
        f"memories: {{first: {f['memories']['first']}, last: {f['memories']['last']}}}",
        f"names: {json.dumps(f['names'])}",
        f"power: {json.dumps(f['power'])}",
        f"bands_mhz: {json.dumps(f['bands_mhz'])}",
        f"modes: {json.dumps(f['modes'])}",
        f"tone_modes: {json.dumps(f['tone_modes'])}",
    ]
    if f["cross_modes"]:
        lines.append(f"cross_modes: {json.dumps(f['cross_modes'])}")
    notes = "Drafted from CHIRP's driver; check against the radio's manual or a CHIRP export."
    if f["other_modes"]:
        notes += f" CHIRP modes rflib doesn't have: {', '.join(f['other_modes'])}."
    if not f["power"]:
        notes += " The driver lists no per-memory power levels."
    if not f["bands_mhz"]:
        notes += " The driver lists no bands, so they aren't checked; add them from the manual."
    lines.append(f"notes: {json.dumps(notes)}")
    today = datetime.date.today().isoformat()
    source = f"CHIRP driver {f['driver']} (chirp {version()}), drafted by tools/chirp_model.py on {today}"
    lines.append(f"source: {json.dumps(source)}")
    return "\n".join(lines) + "\n"


def dump(radios):
    """Every radio's features, for tools/datacheck."""
    out, failed = {}, {}
    for name, cls in sorted(radios.items()):
        try:
            out[name] = features(cls)
        except Exception as e:  # a driver that can't describe itself offline
            failed[name] = f"{type(e).__name__}: {e}"
    return {"chirp_version": version(), "radios": out, "failed": failed}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("radio", nargs="?", help='the CHIRP radio, as --list shows it (such as "Baofeng UV-5R")')
    ap.add_argument("--list", metavar="TEXT", nargs="?", const="", help="list CHIRP radios, optionally only those containing TEXT")
    ap.add_argument("--json", action="store_true", help="print every radio's features as JSON, for tools/datacheck")
    args = ap.parse_args()
    radios = {label(cls): cls for cls in drivers().values()}
    if args.json:
        json.dump(dump(radios), sys.stdout, indent=1, sort_keys=True)
        print()
        return 0
    if args.list is not None:
        for name in sorted(radios):
            if args.list.lower() in name.lower():
                print(name)
        return 0
    if not args.radio:
        ap.error("name a radio, or use --list")
    cls = radios.get(args.radio)
    if cls is None:
        close = [n for n in sorted(radios) if slug(args.radio).replace("-", "") in slug(n).replace("-", "")]
        hint = ("; did you mean: " + ", ".join(close[:10])) if close else "; use --list to find it"
        print(f"no CHIRP radio named {args.radio!r}{hint}", file=sys.stderr)
        return 1
    sys.stdout.write(draft(cls))
    return 0


if __name__ == "__main__":
    sys.exit(main())
