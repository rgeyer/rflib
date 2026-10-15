import { describe, expect, it } from "vitest";
import type { System } from "../api/client";
import { networkLabel, networkURL, planDates, systemLines } from "./records";

describe("networks", () => {
  it("links AllStar nodes to their page and others to their lookup", () => {
    expect(networkURL("allstar:43763")).toBe("https://stats.allstarlink.org/stats/43763");
    expect(networkURL("echolink:12345")).toBe("https://www.echolink.org/validation/node_lookup.jsp");
    expect(networkURL("wires-x:12345")).toBeNull();
    expect(networkLabel("wires-x:12345")).toBe("WIRES-X 12345");
  });
});

describe("systemLines", () => {
  const system = {
    members: [
      { channel_id: "a", linking: "full-time" },
      { channel_id: "b", linking: "full-time" },
      { channel_id: "c", linking: "on-demand" },
      { channel_id: "nowhere", linking: "full-time" },
    ],
  } as System;
  const at: Record<string, [number, number]> = { a: [1, 1], b: [2, 2], c: [3, 3] };
  it("joins every located pair, dashed when either is on demand", () => {
    const lines = systemLines(system, (id) => at[id] ?? null);
    expect(lines).toHaveLength(3);
    expect(lines.map((l) => l.onDemand)).toEqual([false, true, true]);
  });
});

describe("planDates", () => {
  it("reads a plan's dates", () => {
    expect(planDates({ starts: "2026-07-15", ends: "2026-07-26" } as never)).toBe("2026-07-15 to 2026-07-26");
    expect(planDates({ starts: "2026-07-15", ends: null } as never)).toBe("from 2026-07-15");
    expect(planDates({ starts: null, ends: null } as never)).toBe("");
  });
});
