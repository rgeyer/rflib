import { describe, expect, it } from "vitest";
import type { Channel } from "../api/client";
import { sites } from "./map";

const at = (name: string, rx_hz: number, latitude?: number, longitude?: number) =>
  ({ id: name, name, rx_hz, location: latitude == null ? null : { latitude, longitude, radius_km: null } }) as Channel;

describe("sites", () => {
  it("groups channels at one place, to about a meter", () => {
    const s = sites([at("B", 147_300_000, 34.6, -120.4), at("A", 146_940_000, 34.600001, -120.400001), at("C", 146_520_000, 35, -120), at("D", 1)]);
    expect(s.map((x) => x.channels.map((c) => c.name))).toEqual([["A", "B"], ["C"]]);
  });
});
