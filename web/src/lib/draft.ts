import type { ListChanges } from "../api/client";

/** DraftMark is how a draft changes one memory from the saved radio. */
export type DraftMark =
  | { kind: "added" }
  | { kind: "moved"; from: string }
  | { kind: "changed"; changes: string[] }
  | { kind: "renumbered"; from: number | null };

/** draftMarks maps the channel IDs of the memories a draft adds, moves,
 * changes, or renumbers to how. A memory moved to another section is
 * marked moved, whatever else changed about it. */
export function draftMarks(c: ListChanges | undefined): Map<string, DraftMark> {
  const out = new Map<string, DraftMark>();
  if (!c) return out;
  for (const m of c.renumbered) out.set(m.channel.id, { kind: "renumbered", from: m.from });
  for (const x of c.changed) out.set(x.channel.id, { kind: "changed", changes: x.changes });
  for (const m of c.moved) out.set(m.channel.id, { kind: "moved", from: m.from_section });
  for (const x of c.added) out.set(x.id, { kind: "added" });
  return out;
}
