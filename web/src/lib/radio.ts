// Formatting and parsing for radio values, matching rflib's own: exact
// integer Hz for frequencies, and tones as none, ctcss:100.000, dcs:023, or
// dcs:023:inverted.

/** modelLabel names a model in a picker, marking one installed from a
 * file on the server, which may be one sent to try. */
export function modelLabel(m: { name: string; description: string; origin: string }): string {
  const from = { builtin: "", local: ", installed", replaces_builtin: ", installed in place of the built-in", same_as_builtin: ", installed" }[m.origin] ?? "";
  return `${m.description} (${m.name}${from})`;
}

/** formatMHz shows Hz as MHz with six decimals, as rflib does. */
export function formatMHz(hz: number): string {
  const whole = Math.floor(hz / 1_000_000);
  const frac = String(hz % 1_000_000).padStart(6, "0");
  return `${whole}.${frac}`;
}

/** parseMHz reads MHz text into exact Hz, or returns null if it isn't one. */
export function parseMHz(text: string): number | null {
  const m = /^\s*(\d+)(?:\.(\d{0,6}))?\s*$/.exec(text);
  if (!m) return null;
  const hz = Number(m[1]) * 1_000_000 + Number((m[2] ?? "").padEnd(6, "0"));
  return hz > 0 ? hz : null;
}

/** duplex classifies a channel's frequencies as rflib does. */
export function duplex(rx: number, tx: number | null): string {
  if (tx === null) return "off";
  if (tx === rx) return "simplex";
  if (Math.abs(tx - rx) >= 70_000_000) return "split";
  return tx > rx ? "+" : "-";
}

/** offsetMHz is a repeater offset's size, for + and - channels. */
export function offsetMHz(rx: number, tx: number | null): string {
  const d = duplex(rx, tx);
  return d === "+" || d === "-" ? formatMHz(Math.abs((tx as number) - rx)) : "";
}

export type Tone =
  | { kind: "none" }
  | { kind: "ctcss"; hz: string }
  | { kind: "dcs"; code: string; inverted: boolean };

/** parseTone reads rflib's tone syntax. */
export function parseTone(text: string): Tone {
  const [kind, value, polarity] = text.split(":");
  if (kind === "ctcss" && value) return { kind: "ctcss", hz: String(Number(value)) };
  if (kind === "dcs" && value) return { kind: "dcs", code: value.padStart(3, "0"), inverted: polarity === "inverted" };
  return { kind: "none" };
}

/** formatTone writes rflib's tone syntax. */
export function formatTone(t: Tone): string {
  switch (t.kind) {
    case "ctcss":
      return `ctcss:${Number(t.hz).toFixed(3)}`;
    case "dcs":
      return `dcs:${t.code.padStart(3, "0")}${t.inverted ? ":inverted" : ""}`;
    default:
      return "none";
  }
}

/** shortTone shows one tone as repeater lists do: 100.0 or D023N. */
export function shortTone(text: string): string {
  const t = parseTone(text);
  if (t.kind === "ctcss") return Number(t.hz).toFixed(1);
  if (t.kind === "dcs") return `D${t.code}${t.inverted ? "I" : "N"}`;
  return "";
}

/** toneSummary shows a channel's tones as programmed, as repeater lists do:
 * T100.0 (encode), TSQL 100.0 (encode and squelch), or T100.0 R88.5. */
export function toneSummary(c: { tx_tone: string; rx_tone: string; tone_squelch: boolean }): string {
  const t = shortTone(c.tx_tone);
  const r = c.tone_squelch ? shortTone(c.rx_tone) : "";
  if (!t && !r) return "";
  if (!r) return `T${t}`;
  if (!t) return `R${r}`;
  return t === r ? `TSQL ${t}` : `T${t} R${r}`;
}

/** The 50 standard CTCSS tones, in Hz. */
export const CTCSS = [
  "67.0", "69.3", "71.9", "74.4", "77.0", "79.7", "82.5", "85.4", "88.5", "91.5",
  "94.8", "97.4", "100.0", "103.5", "107.2", "110.9", "114.8", "118.8", "123.0", "127.3",
  "131.8", "136.5", "141.3", "146.2", "151.4", "156.7", "159.8", "162.2", "165.5", "167.9",
  "171.3", "173.8", "177.3", "179.9", "183.5", "186.2", "189.9", "192.8", "196.6", "199.5",
  "203.5", "206.5", "210.7", "218.1", "225.7", "229.1", "233.6", "241.8", "250.3", "254.1",
];

/** The 104 standard DCS codes. */
export const DCS = [
  "023", "025", "026", "031", "032", "036", "043", "047", "051", "053", "054", "065", "071", "072", "073", "074",
  "114", "115", "116", "122", "125", "131", "132", "134", "143", "145", "152", "155", "156", "162", "165", "172", "174",
  "205", "212", "223", "225", "226", "243", "244", "245", "246", "251", "252", "255", "261", "263", "265", "266", "271", "274",
  "306", "311", "315", "325", "331", "332", "343", "346", "351", "356", "364", "365", "371",
  "411", "412", "413", "423", "431", "432", "445", "446", "452", "454", "455", "462", "464", "465", "466",
  "503", "506", "516", "523", "526", "532", "546", "565",
  "606", "612", "624", "627", "631", "632", "654", "662", "664",
  "703", "712", "723", "731", "732", "734", "743", "754",
];

export const MODES = ["FM", "NFM", "AM", "USB", "LSB", "CW"];
export const SERVICES = ["", "amateur", "gmrs", "frs", "cb", "murs", "marine", "noaa", "public_safety", "other"];
export const POWERS = ["", "low", "medium", "high", "max"];

/** Programmed is what a comparison looks at in a memory or a channel. */
type Programmed = { name: string; rx_hz: number; tx_hz?: number | null; mode: string; tx_tone: string; rx_tone: string; tone_squelch: boolean };

/** fieldValue is one field of a memory or channel, named as a comparison
 * names what differs ("rx", "tx tone", and so on), for showing. */
export function fieldValue(v: Programmed, field: string): string {
  switch (field) {
    case "name":
      return v.name;
    case "rx":
      return formatMHz(v.rx_hz);
    case "tx":
      return v.tx_hz == null ? "none" : formatMHz(v.tx_hz);
    case "mode":
      return v.mode;
    case "tx tone":
      return shortTone(v.tx_tone) || "none";
    case "rx tone":
      return shortTone(v.rx_tone) || "none";
    case "tone squelch":
      return v.tone_squelch ? "on" : "off";
  }
  return "";
}
