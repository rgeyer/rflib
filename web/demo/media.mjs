// Makes the README's screenshots and recording from a demo database:
//
//   make web
//   go build -buildvcs=false -ldflags "-X github.com/ryan/rflib/internal/buildinfo.Version=v0.1.0" -o rflib ./cmd/rflib
//   cd web && node demo/media.mjs --shared shared.json --out ../docs/media
//
// The version stamp makes the sidebar read as a release does.
//
// --shared is a shared list export (rflib shared export json) with the
// Central Coast repeaters the demo's searches expect, as the hosted
// instance's is. The recording is a webm; demo/README.md says how to make
// the README's GIF from it.

import { spawn, execFileSync } from "node:child_process";
import {
  copyFileSync,
  mkdtempSync,
  mkdirSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { parseArgs } from "node:util";
import { chromium } from "@playwright/test";
import { client, populate } from "./populate.mjs";

const { values } = parseArgs({
  options: {
    shared: { type: "string" },
    out: { type: "string", default: "../docs/media" },
    rflib: { type: "string", default: "../rflib" },
    port: { type: "string", default: "18510" },
  },
});
if (!values.shared) {
  console.error(
    "usage: node demo/media.mjs --shared shared.json [--out ../docs/media] [--rflib ../rflib]",
  );
  process.exit(2);
}
const out = resolve(values.out);
mkdirSync(out, { recursive: true });
const url = `http://127.0.0.1:${values.port}`;
const work = mkdtempSync(join(tmpdir(), "rflib-demo-"));
const db = join(work, "library.db");
const rflib = (...args) =>
  execFileSync(resolve(values.rflib), ["--db", db, ...args], {
    stdio: ["ignore", "ignore", "inherit"],
  });

// A database like the hosted one: the shared list from the export, and the
// built-in lists brought up to date.
rflib("init", "--no-seed");
rflib(
  "shared",
  "import",
  "json",
  resolve(values.shared),
  "--reason",
  "the demo's shared list",
);
rflib("seed", "update");
const server = spawn(
  resolve(values.rflib),
  ["--db", db, "serve", "--addr", `127.0.0.1:${values.port}`],
  { stdio: "ignore" },
);

/** pause holds a shot in the recording long enough to read. */
const pause = (page, ms = 1200) => page.waitForTimeout(ms);
/** caption shows a line of text over the page, for the recording. */
async function caption(page, text) {
  await page.evaluate((t) => {
    let el = document.getElementById("demo-caption");
    if (!el) {
      el = document.createElement("div");
      el.id = "demo-caption";
      el.style.cssText =
        "position:fixed;left:50%;bottom:28px;transform:translateX(-50%);z-index:100000;padding:10px 18px;border-radius:8px;" +
        "background:rgba(20,24,32,.88);color:#fff;font:600 18px/1.3 system-ui,sans-serif;box-shadow:0 4px 16px rgba(0,0,0,.25);pointer-events:none";
      document.body.append(el);
    }
    el.textContent = t;
    el.style.display = t ? "block" : "none";
  }, text);
}

try {
  for (let i = 0; ; i++) {
    try {
      if ((await fetch(`${url}/api/v1/health`)).ok) break;
    } catch {
      if (i > 50) throw new Error("rflib serve didn't start");
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  await populate(await client(url));
  const browser = await chromium.launch();

  // --- Screenshots, at twice the pixels for sharp text ---
  const shots = await browser.newContext({
    viewport: { width: 1600, height: 900 },
    deviceScaleFactor: 2,
    baseURL: url,
    colorScheme: "light",
  });
  const page = await shots.newPage();
  const shot = (name) => page.screenshot({ path: join(out, `${name}.png`) });

  await page.goto("/l/default/radios/HT");
  await page
    .getByRole("row", { name: /^Select memory.*SntMr147\.30/ })
    .waitFor();
  await shot("memories");

  // A draft: take a repeater off, add one, and turn off scanning on another.
  await page
    .getByRole("row", { name: /^Select memory.*Tpsqt440\.125/ })
    .getByRole("button", { name: "Take off this radio" })
    .click();
  await page.getByText(/^Draft since/).waitFor();
  const call = await client(url);
  await call("POST", "/libraries/default/radios/HT/channels", {
    query: 'name:"Figueroa 147.21"',
    section: "Repeaters",
  });
  await page.reload();
  const scan = page.getByRole("switch", { name: "Scan Cal Poly 442.30" });
  await page.getByRole("row", { name: /^Select memory.*Fgr147\.21/ }).waitFor();
  for (let i = 0; i < 5 && (await scan.isChecked()); i++) {
    await scan.click({ force: true });
    await page.waitForTimeout(800);
  }
  await page.getByText(/So far: .*1 changed/).waitFor();
  // Tall enough to show the added memory and the one taken off.
  await page.setViewportSize({ width: 1600, height: 1640 });
  await shot("draft");
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.getByRole("button", { name: "Discard draft" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Discard draft" })
    .click();
  await page.getByText(/it's as it was/).waitFor();
  await page
    .getByText(/it's as it was/)
    .waitFor({ state: "hidden", timeout: 15000 });

  // Compare with the radio's file, as if two memories were changed on the
  // radio: a tone, and a power level.
  const csv = await call(
    "GET",
    "/libraries/default/radios/HT/export?skip_invalid=true",
  );
  const edited = csv
    .split("\n")
    .map((l) =>
      l.includes("SntMr147.30")
        ? l.replace(",Tone,131.8,", ",Tone,100.0,")
        : l.includes("OrctHl145.14")
          ? l.replace(/,8W,/, ",1.0W,")
          : l,
    )
    .join("\n");
  const file = join(work, "HT from the radio.csv");
  writeFileSync(file, edited);
  await page.getByRole("tab", { name: "Compare" }).click();
  const chooser = page.waitForEvent("filechooser");
  await page.getByLabel("The radio's file", { exact: true }).click();
  await (await chooser).setFiles(file);
  await page.getByText(/\d+ changed/).waitFor();
  await page.getByRole("table").first().scrollIntoViewIfNeeded();
  await shot("compare");

  await page.goto("/l/default/map");
  await page.getByLabel("Search", { exact: true }).fill("in:all");
  await page.getByText(/channels with a location/).waitFor();
  await page.waitForTimeout(2500); // map tiles
  await shot("map");
  await shots.close();

  // --- The recording: one correction, every radio knows ---
  const rec = await browser.newContext({
    viewport: { width: 1280, height: 760 },
    baseURL: url,
    recordVideo: { dir: work, size: { width: 1280, height: 760 } },
  });
  const v = await rec.newPage();
  await v.goto("/l/default/radios");
  await v.getByRole("link", { name: "HT", exact: true }).waitFor();
  await caption(v, "Two radios, built from one channel list, both programmed");
  await pause(v, 2600);
  await v.getByRole("link", { name: "HT", exact: true }).click();
  await v.getByRole("row", { name: /^Select memory.*SntMr147\.30/ }).waitFor();
  await caption(
    v,
    "The HT's memories: repeaters near Santa Maria, weather, and calling",
  );
  await pause(v, 2800);

  await v.goto("/l/shared/channels");
  await caption(
    v,
    "The Santa Maria repeater changes its tone from 131.8 to 100.0",
  );
  await v
    .getByLabel("Search", { exact: true })
    .pressSequentially('name:"Santa Maria"', { delay: 40 });
  await v
    .getByRole("row", { name: /Santa Maria 147\.30/ })
    .getByText(/Santa Maria 147\.30/)
    .click();
  await pause(v, 900);
  const tone = v.getByRole("combobox", { name: "Transmit tone CTCSS tone" });
  await tone.click();
  await tone.fill("100.0");
  await v.getByRole("option", { name: "100.0", exact: true }).click();
  await pause(v, 900);
  await v.getByRole("button", { name: "Save correction" }).click();
  await v
    .getByRole("textbox", { name: "Reason" })
    .pressSequentially("The club changed its PL tone", { delay: 30 });
  await v
    .getByRole("dialog")
    .getByRole("button", { name: "Save", exact: true })
    .click();
  await v.getByText("Corrected; links follow").waitFor();
  await caption(v, "Corrected once, for everyone who uses it");
  await pause(v, 2000);

  await v.goto("/l/default/radios");
  await v.getByText("Needs programming", { exact: true }).first().waitFor();
  await caption(v, "Every radio with that repeater now needs programming");
  await pause(v, 3000);
  await v.getByRole("link", { name: "HT", exact: true }).click();
  await v.getByRole("button", { name: "Export", exact: true }).waitFor();
  await caption(v, "Export the HT's file for CHIRP, and program it");
  await pause(v, 1500);
  const download = v.waitForEvent("download");
  await v.getByRole("button", { name: "Export", exact: true }).click();
  // The radio changed since its last revision, so the export asks why.
  const reason = v.getByRole("dialog").getByRole("textbox", { name: "Reason" });
  await reason.pressSequentially("New Santa Maria tone", { delay: 30 });
  await v
    .getByRole("dialog")
    .getByRole("button", { name: "Save", exact: true })
    .click();
  await download;
  await pause(v, 1800);
  await v.goto("/l/default/radios");
  await caption(v, "The HT is up to date; the truck is next");
  await pause(v, 3000);
  const video = v.video();
  await rec.close();
  copyFileSync(await video.path(), join(out, "demo.webm"));
  await browser.close();
  console.log(`Wrote ${readdirSync(out).join(", ")} to ${out}`);
} finally {
  server.kill();
  rmSync(work, { recursive: true, force: true });
}
