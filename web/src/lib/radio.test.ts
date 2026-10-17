import { describe, expect, it } from "vitest";
import { duplex, fieldValue, formatMHz, formatTone, offsetMHz, parseMHz, parseTone, toneSummary } from "./radio";

describe("frequencies", () => {
  it("round-trips exactly", () => {
    expect(parseMHz("146.52")).toBe(146_520_000);
    expect(parseMHz("462.5625")).toBe(462_562_500);
    expect(formatMHz(462_562_500)).toBe("462.562500");
    expect(parseMHz("0")).toBeNull();
    expect(parseMHz("146.5200001")).toBeNull();
    expect(parseMHz("abc")).toBeNull();
  });
  it("classifies duplex", () => {
    expect(duplex(146_940_000, 146_340_000)).toBe("-");
    expect(offsetMHz(146_940_000, 146_340_000)).toBe("0.600000");
    expect(duplex(146_520_000, null)).toBe("off");
    expect(duplex(146_000_000, 446_000_000)).toBe("split");
  });
});

describe("tones", () => {
  it("round-trips rflib's syntax", () => {
    expect(formatTone(parseTone("ctcss:100.000"))).toBe("ctcss:100.000");
    expect(formatTone(parseTone("dcs:023:inverted"))).toBe("dcs:023:inverted");
    expect(formatTone({ kind: "ctcss", hz: "131.8" })).toBe("ctcss:131.800");
  });
  it("summarizes as programmed", () => {
    expect(toneSummary({ tx_tone: "ctcss:100.000", rx_tone: "ctcss:100.000", tone_squelch: true })).toBe("TSQL 100.0");
    expect(toneSummary({ tx_tone: "ctcss:131.800", rx_tone: "ctcss:88.500", tone_squelch: false })).toBe("T131.8");
    expect(toneSummary({ tx_tone: "none", rx_tone: "dcs:023", tone_squelch: true })).toBe("RD023N");
  });
});

describe("fieldValue", () => {
  it("shows what a comparison names", () => {
    const v = { name: "Club", rx_hz: 147_300_000, tx_hz: null, mode: "FM", tx_tone: "ctcss:131.800", rx_tone: "none", tone_squelch: false };
    expect(fieldValue(v, "tx tone")).toBe("131.8");
    expect(fieldValue(v, "rx tone")).toBe("none");
    expect(fieldValue(v, "tx")).toBe("none");
    expect(fieldValue(v, "rx")).toBe("147.300000");
    expect(fieldValue(v, "tone squelch")).toBe("off");
  });
});
