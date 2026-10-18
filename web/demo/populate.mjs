// Fills an empty library with rflib's demo: two radios built from the
// shared list by searches, one channel of the library's own, and both
// radios exported, so a correction to the shared list shows them needing
// programming. It works on a server with or without accounts:
//
//   node demo/populate.mjs --url https://rflib.example.org --user demo --password '...'
//   node demo/populate.mjs --url http://127.0.0.1:8080
//
// Its searches assume a shared list with Central Coast California
// repeaters near Santa Maria, as the hosted instance's is.

import { parseArgs } from "node:util";

/** Around Santa Maria, CA: where the demo's radios live. */
export const NEAR = "near:34.95,-120.44,45";

/** client talks to rflib's API, signed in when given an account. */
export async function client(url, user, password) {
  const base = url.replace(/\/$/, "") + "/api/v1";
  let cookie = "";
  async function call(method, path, body) {
    const res = await fetch(base + path, {
      method,
      headers: {
        "Content-Type": "application/json",
        ...(cookie && { Cookie: cookie }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
    const type = res.headers.get("content-type") ?? "";
    return type.includes("json") ? JSON.parse(text) : text;
  }
  if (user) {
    const res = await fetch(base + "/auth/signin", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: user, password }),
    });
    if (!res.ok)
      throw new Error(
        `signing in as ${user}: ${res.status} ${await res.text()}`,
      );
    cookie = (res.headers.get("set-cookie") ?? "").split(";")[0];
  }
  return call;
}

/** populate builds the demo in the signed-in account's library (or the
 * only library, without accounts), refusing one that already has radios. */
export async function populate(call) {
  const { libraries } = await call("GET", "/libraries");
  const mine = libraries.find((l) => !l.shared);
  const lib = `/libraries/${encodeURIComponent(mine.name)}`;
  const { radios } = await call("GET", `${lib}/radios`);
  if (radios.length)
    throw new Error(
      `${mine.name} already has radios; populate an empty library`,
    );

  // One channel of the demo's own: a club's Field Day talk-in.
  const own = await call("POST", `${lib}/channels`, {
    name: "Field Day Talk-in",
    rx_hz: 146_550_000,
    tx_hz: 146_550_000,
    mode: "FM",
    service: "amateur",
    notes: "Talk-in for the club's Field Day site",
  });

  const radio = async (name, model, power, sections) => {
    await call("POST", `${lib}/radios`, { name, model, default_power: power });
    const r = `${lib}/radios/${encodeURIComponent(name)}`;
    await call("POST", `${r}/draft`);
    for (const [section, query, sort] of sections) {
      await call("POST", `${r}/sections`, { name: section });
      await call("POST", `${r}/sources`, { query, section, sort });
    }
    await call("POST", `${r}/sync`, {});
    return r;
  };
  const ht = await radio("HT", "baofeng-uv5rm", "high", [
    ["Calling", "tag:calling (band:2m OR band:70cm) mode:FM", "freq"],
    [
      "Repeaters",
      `is:repeater ${NEAR} (band:2m OR band:70cm) -status:retired`,
      "name",
    ],
    ["Weather", "band:noaa", "name"],
  ]);
  await call("POST", `${ht}/channels`, {
    ids: [own.channel?.id ?? own.id],
    section: "Calling",
  });
  const truck = await radio("Truck", "yaesu-ftm150", "medium", [
    [
      "Repeaters",
      `is:repeater ${NEAR} (band:2m OR band:70cm) -status:retired`,
      "name",
    ],
    ["Winlink", `tag:winlink ${NEAR} band:2m`, "freq"],
    ["Weather", "band:noaa", "name"],
  ]);
  for (const r of [ht, truck]) {
    await call("POST", `${r}/draft/save`);
    await call(
      "GET",
      `${r}/export?skip_invalid=true&reason=${encodeURIComponent("programmed the radio")}`,
    );
  }
  return mine.name;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const { values } = parseArgs({
    options: {
      url: { type: "string" },
      user: { type: "string" },
      password: { type: "string" },
    },
  });
  if (!values.url) {
    console.error(
      "usage: node demo/populate.mjs --url URL [--user NAME --password PASSWORD]",
    );
    process.exit(2);
  }
  const call = await client(values.url, values.user, values.password);
  console.log(`Built the demo in ${await populate(call)}`);
}
