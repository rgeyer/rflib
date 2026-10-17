import { describe, expect, it } from "vitest";
import type { Channel, ListChanges } from "../api/client";
import { draftMarks } from "./draft";

const ch = (id: string) => ({ id }) as Channel;
const move = (id: string, from_section: string, to_section: string) => ({ channel: ch(id), from: 3, to: 9, from_section, to_section });

describe("draftMarks", () => {
  it("marks each memory once, the move winning", () => {
    const c = {
      added: [ch("a")],
      removed: [ch("r")],
      changed: [{ channel: ch("c"), changes: ["power: low → high"] }, { channel: ch("m"), changes: ["scan: on → off"] }],
      moved: [move("m", "Calling", "Repeaters")],
      renumbered: [move("n", "Calling", "Calling")],
      sections_added: [],
      sections_removed: [],
      sources_added: [],
      sources_removed: [],
    } as ListChanges;
    const marks = draftMarks(c);
    expect(marks.get("a")).toEqual({ kind: "added" });
    expect(marks.get("c")).toEqual({ kind: "changed", changes: ["power: low → high"] });
    expect(marks.get("m")).toEqual({ kind: "moved", from: "Calling" });
    expect(marks.get("n")).toEqual({ kind: "renumbered", from: 3 });
    expect(marks.has("r")).toBe(false);
    expect(draftMarks(undefined).size).toBe(0);
  });
});
