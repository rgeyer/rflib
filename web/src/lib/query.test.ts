import { describe, expect, it } from "vitest";
import { getTerm, setTerm, words } from "./query";

describe("guided search terms", () => {
  it("splits at top-level spaces only", () => {
    expect(words('band:2m tag:"club net" (is:repeater OR band:70cm) -tag:hf')).toEqual([
      "band:2m",
      'tag:"club net"',
      "(is:repeater OR band:70cm)",
      "-tag:hf",
    ]);
  });
  it("adds, replaces, and removes a field's term", () => {
    expect(setTerm("", "band", "2m")).toBe("band:2m");
    expect(setTerm("repeater band:2m", "band", "70cm")).toBe("repeater band:70cm");
    expect(setTerm("band:2m is:repeater", "band", null)).toBe("is:repeater");
    expect(setTerm("x", "tag", "club net")).toBe('x tag:"club net"');
  });
  it("reads a term, ignoring negated and grouped ones", () => {
    expect(getTerm('tag:"club net" band:2m', "tag")).toBe("club net");
    expect(getTerm("-band:2m (band:70cm OR x)", "band")).toBeNull();
    expect(setTerm("-band:2m", "band", "6m")).toBe("-band:2m band:6m");
  });
});
