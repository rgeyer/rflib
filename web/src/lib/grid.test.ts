import { describe, expect, it } from "vitest";
import { parseGrid } from "./grid";

describe("parseGrid", () => {
  it("finds a square's center and reach", () => {
    expect(parseGrid("cm94sq")).toEqual({ latitude: 34.6875, longitude: -120.458333, radius_km: 4.5 });
    expect(parseGrid("CM94")?.latitude).toBe(34.5);
    expect(parseGrid("CM94sq12")?.longitude).toBe(-120.4875);
  });
  it("rejects what isn't a square", () => {
    for (const bad of ["", "CM9", "CM94s", "SM94", "CM94sz", "12AB"]) expect(parseGrid(bad)).toBeNull();
  });
});
