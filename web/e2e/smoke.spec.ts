import { expect, test } from "@playwright/test";

// One pass through the main screens against a real rflib serve: link
// channels from the shared list, add and edit a channel, set up a radio,
// sync it, and export its file.
test("lists, channels, radios, and export", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/l\/default\/channels$/);
  await expect(page.getByText("0 channels")).toBeVisible();

  // The Shared list item opens the shared list, marked in the header; the
  // other items always lead back to the user's own library.
  await page.getByRole("link", { name: /Shared list/ }).click();
  await expect(page).toHaveURL(/\/l\/shared\/channels$/);
  await expect(page.getByRole("banner").getByText("Shared list")).toBeVisible();
  await page.getByRole("link", { name: "Channels", exact: true }).click();
  await expect(page).toHaveURL(/\/l\/default\/channels$/);

  // A new database's shared list starts with the built-in lists, all up
  // to date, so there's nothing to install.
  await page.getByRole("link", { name: /Built-in lists/ }).click();
  const noaa = page.locator(".mantine-Card-root", { hasText: "us-noaa" });
  await expect(noaa.getByText("Up to date")).toBeVisible();
  await expect(noaa.getByRole("button")).toHaveCount(0);

  // Tag NOAA weather radio in the shared list: the tags are the user's own,
  // and put the seven channels in My library.
  await page.getByRole("link", { name: "Channels", exact: true }).click();
  await page.getByLabel("Search", { exact: true }).fill("service:noaa");
  await page.getByRole("button", { name: "Source filter" }).click();
  await page.getByRole("menuitem", { name: "the shared list", exact: true }).click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue(/in:shared/);
  await expect(page.getByText("7 channels")).toBeVisible();
  await page.getByRole("checkbox", { name: "Select all" }).click();
  await page.getByRole("button", { name: "Add tags" }).click();
  await page.getByRole("combobox", { name: "Tags" }).fill("home");
  await page.getByRole("combobox", { name: "Tags" }).press("Enter");
  await page.getByRole("combobox", { name: "Tags" }).press("Tab");
  await page.getByRole("dialog").getByRole("button", { name: "Add on 7 channels" }).click();
  await expect(page.getByText("Updated 7 channels")).toBeVisible();
  await page.getByRole("button", { name: "Source filter" }).click();
  await page.getByRole("menuitem", { name: "in use (default)" }).click();
  await page.getByLabel("Search", { exact: true }).fill("");

  // Add a repeater with a tone.
  await expect(page.getByText("7 channels", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New channel" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Marian");
  await page.getByRole("textbox", { name: "Receive MHz" }).fill("147.3");
  await page.getByRole("textbox", { name: "Transmit MHz" }).fill("147.9");
  await page.getByRole("dialog").getByText("CTCSS", { exact: true }).first().click();
  await page.getByRole("combobox", { name: "Transmit tone CTCSS tone" }).fill("131.8");
  await page.getByRole("option", { name: "131.8", exact: true }).click();
  // A grid square fills in the location.
  await page.getByRole("textbox", { name: "Grid square" }).fill("CM94sq");
  await expect(page.getByRole("textbox", { name: "Latitude" })).toHaveValue("34.6875");
  await expect(page.getByRole("textbox", { name: "Radius km" })).toHaveValue("4.5");
  await page.getByRole("button", { name: "Add channel" }).click();
  await expect(page.getByText("Channel added")).toBeVisible();
  const row = page.getByRole("row", { name: /Marian/ });
  await expect(row).toContainText("147.300000");
  await expect(row).toContainText("T131.8");
  await page.getByLabel("Search", { exact: true }).fill("near:cm94sq,1");
  await expect(page.getByText("1 channel", { exact: true })).toBeVisible();
  await page.getByLabel("Search", { exact: true }).fill("");

  // Search narrows the table; a guided filter writes its term into the search.
  await page.getByLabel("Search", { exact: true }).fill("band:noaa");
  await expect(page.getByText("7 channels")).toBeVisible();
  await page.getByLabel("Search", { exact: true }).fill("");
  await page.getByRole("button", { name: "Band filter" }).click();
  await page.getByRole("menuitem", { name: "noaa" }).click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("band:noaa");
  await expect(page.getByText("7 channels")).toBeVisible();
  await page.getByLabel("Search", { exact: true }).fill("");

  // Set the service on several channels; the links among them are left out.
  await page.getByRole("checkbox", { name: "Select Marian" }).click();
  await page.getByRole("checkbox", { name: "Select NOAA WX1" }).click();
  await page.getByRole("button", { name: "Set service" }).click();
  await page.getByRole("dialog").getByRole("combobox", { name: "Service" }).click();
  await page.getByRole("option", { name: "amateur" }).click();
  await expect(page.getByText("1 of them are shared channels")).toBeVisible();
  await page.getByRole("button", { name: "Set on 1 channels" }).click();
  await expect(page.getByText("Set the service on 1 channels")).toBeVisible();
  await expect(page.getByRole("row", { name: /Marian/ })).toContainText("amateur");

  // A shared channel's shared fields can't be edited; power and scan are per radio.
  await page.getByRole("row", { name: /WX1/ }).getByText(/WX1/).click();
  await expect(page.getByRole("dialog").getByText("A shared channel", { exact: true })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Receive MHz" })).toBeDisabled();
  await expect(page.getByRole("dialog").getByRole("textbox", { name: "Power" })).toHaveCount(0);
  await page.keyboard.press("Escape");

  // A radio with a source; sync adds the matches.
  await page.getByRole("link", { name: "Radios" }).click();
  await page.getByRole("button", { name: "Add radio" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("HT");
  await page.getByRole("combobox", { name: "Model" }).click();
  await page.getByRole("option", { name: /baofeng-uv5rm/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Add" }).click();
  await expect(page).toHaveURL(/\/radios\/HT$/);
  await page.getByRole("tab", { name: "Sources" }).click();
  await page.getByLabel("New source query", { exact: true }).fill("band:noaa OR name:Marian");
  await page.getByRole("button", { name: "Add source" }).click();
  await expect(page.getByText("Source added")).toBeVisible();
  await page.getByRole("button", { name: "Sync" }).click();
  await expect(page.getByText("8 to add")).toBeVisible();
  await page.getByRole("button", { name: "Sync now" }).click();
  await expect(page.getByText("Added 8 channels")).toBeVisible();
  await page.getByRole("tab", { name: "Memories" }).click();
  // The source's section heads its memories.
  await expect(page.locator("tr[data-section]", { hasText: "band:noaa OR name:Marian" })).toBeVisible();
  await expect(page.getByRole("row", { name: /^Select memory.*Marian/ })).toBeVisible();

  // Export downloads the CHIRP file.
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export", exact: true }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe("HT.csv");
  await expect(page.getByText("Exported 8 memories")).toBeVisible();

  // Bulk changes: search the memories, select a range with shift-click,
  // turn scan off, then take them off the radio.
  await page.getByRole("button", { name: "Band filter" }).click();
  await page.getByRole("menuitem", { name: "noaa" }).click();
  await expect(page.getByText("7 of 8 memories")).toBeVisible();
  const boxes = page.getByRole("checkbox", { name: /^Select memory/ });
  await boxes.first().click();
  await boxes.last().click({ modifiers: ["Shift"] });
  await expect(page.getByText("7 selected")).toBeVisible();
  await page.getByRole("button", { name: "Scan", exact: true }).click();
  await page.getByRole("menuitem", { name: "Off" }).click();
  await expect(page.getByText("Changed 7 memories")).toBeVisible();
  await page.getByRole("button", { name: "Scan filter" }).click();
  await page.getByRole("menuitem", { name: "off", exact: true }).click();
  await expect(page.getByText("7 of 8 memories")).toBeVisible();
  await page.getByRole("button", { name: "Take off radio" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Take off" }).click();
  await expect(page.getByText("Took 7 channels off HT")).toBeVisible();
  await expect(page.getByText("0 of 1 memories")).toBeVisible();
});

test("the map shows located channels", async ({ page }) => {
  await page.goto("/l/default/map");
  await expect(page.getByRole("heading", { name: "Map" })).toBeVisible();
  await expect(page.locator(".leaflet-container")).toBeVisible();
});

// Runs after the first test, on the same server: organizations, a system,
// and a plan in the shared list, and the plan programmed onto a radio.
test("organizations, systems, and plans", async ({ page, request }) => {
  const saveWithReason = async (reason: string) => {
    await page.getByRole("textbox", { name: "Reason" }).fill(reason);
    await page.getByRole("dialog").getByRole("button", { name: "Save" }).click();
  };

  await page.goto("/l/default/orgs");
  await page.getByRole("button", { name: "New organization" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("SBARC");
  await page.getByRole("textbox", { name: "Callsign" }).fill("w6sba");
  await saveWithReason("pilot");
  await expect(page.getByRole("heading", { name: "SBARC" })).toBeVisible();
  await expect(page.getByText("W6SBA")).toBeVisible();

  // A system, with a member picked from the shared list.
  await page.getByRole("link", { name: "Systems" }).click();
  await page.getByRole("button", { name: "New system" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Hub");
  await page.getByRole("combobox", { name: "The system's own nodes" }).fill("allstar:43763");
  await page.keyboard.press("Enter");
  await saveWithReason("pilot");
  await expect(page.getByRole("heading", { name: "Hub" })).toBeVisible();
  await expect(page.getByRole("link", { name: "AllStar 43763" })).toHaveAttribute("href", "https://stats.allstarlink.org/stats/43763");
  await page.getByLabel("Add a member").fill("name:70cm");
  await page.getByRole("option", { name: /70cm FM Call/ }).click();
  await saveWithReason("pilot");
  await expect(page.getByRole("row", { name: /70cm FM Call/ })).toContainText("full-time");

  // A plan: a channel renamed for the event, and the system as a section.
  await page.getByRole("link", { name: "Plans" }).click();
  await page.getByRole("button", { name: "New plan" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Fair");
  await saveWithReason("pilot");
  await expect(page.getByRole("heading", { name: "Fair" })).toBeVisible();
  await page.getByLabel("Add a shared channel").fill("name:2m mode:fm");
  await page.getByRole("option", { name: /2m FM Call/ }).click();
  await page.getByRole("textbox", { name: "Name in the plan" }).fill("FAIR TAC1");
  await page.getByRole("textbox", { name: "Assignment" }).fill("Tactical");
  await saveWithReason("pilot");
  await expect(page.getByRole("row", { name: /FAIR TAC1/ }).first()).toBeVisible();
  await page.getByRole("textbox", { name: "Add a query section" }).fill("system:hub");
  await page.getByRole("button", { name: "Add section" }).click();
  await page.getByRole("textbox", { name: "Starts at Ch #" }).fill("10");
  await saveWithReason("pilot");
  await expect(page.getByRole("row", { name: /^10 70cm FM Call/ })).toBeVisible();

  // The plan on a radio, in a section from memory 50, matching its numbers.
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Mobile", model: "baofeng-uv5rm" } });
  await request.post("/api/v1/libraries/default/radios/Mobile/sections", { data: { name: "Fair plan", start: 50 } });
  await page.goto("/l/default/radios/Mobile");
  await page.getByRole("tab", { name: "Sources" }).click();
  await page.getByRole("combobox", { name: "or a plan" }).click();
  await page.getByRole("option", { name: "Fair" }).click();
  await page.getByRole("combobox", { name: "Plan numbers" }).click();
  await page.getByRole("option", { name: "Match the plan's" }).click();
  await page.getByRole("combobox", { name: "Section" }).click();
  await page.getByRole("option", { name: "Fair plan" }).click();
  await page.getByRole("button", { name: "Add source" }).click();
  await expect(page.getByRole("cell", { name: "Plan: Fair" })).toBeVisible();
  await page.getByRole("button", { name: "Sync" }).click();
  await expect(page.getByText(/plan Fair\): 2 to add/)).toBeVisible();
  await page.getByRole("button", { name: "Sync now" }).click();
  await page.getByRole("tab", { name: "Memories" }).click();
  await expect(page.getByRole("row", { name: /^Select memory.*FAIR TAC1/ })).toContainText("50");
  await expect(page.getByRole("row", { name: /^Select memory.*70cm FM Call/ })).toContainText("59");
  await expect(page.getByRole("row", { name: /^Select memory.*70cm FM Call/ })).toContainText("Ch 10");
});

// Runs after the first test: from the shared list, link a channel onto a
// radio, then correct it as the maintainer.
test("the shared list: add to a radio, correct, and take off the air", async ({ page }) => {
  const saveWithReason = async (reason: string) => {
    await page.getByRole("textbox", { name: "Reason" }).fill(reason);
    await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click();
  };
  // A shared channel goes straight onto a radio, with nothing to link.
  await page.goto("/l/default/channels");
  await page.getByLabel("Search", { exact: true }).fill("name:wx7");
  await page.getByRole("button", { name: "Source filter" }).click();
  await page.getByRole("menuitem", { name: "the shared list", exact: true }).click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue(/in:shared/);
  await expect(page.getByText("1 channel", { exact: true })).toBeVisible();
  await page.getByRole("checkbox", { name: "Select NOAA WX7" }).click();
  await page.getByRole("button", { name: "Add to radio" }).click();
  await page.getByRole("dialog").getByRole("combobox").click();
  await page.getByRole("option", { name: "HT" }).click();
  await page.getByRole("dialog").getByRole("button", { name: /^Add/ }).click();
  await expect(page.getByText(/Added [01] channels to HT/)).toBeVisible();

  // The maintainer corrects it in the shared list.
  await page.goto("/l/shared/channels");
  await page.getByLabel("Search", { exact: true }).fill("name:wx1");
  await expect(page.getByText("1 channel", { exact: true })).toBeVisible();

  // A correction of a shared field, then a status, each with a reason.
  await page.getByRole("row", { name: /WX1/ }).getByText(/WX1/).click();
  await page.getByRole("textbox", { name: "Notes" }).fill("Mt. Example transmitter");
  await page.getByRole("button", { name: "Save correction" }).click();
  await saveWithReason("site name");
  await expect(page.getByText("Corrected; links follow")).toBeVisible();
  await page.getByRole("row", { name: /WX1/ }).getByText(/WX1/).click();
  await page.getByText("Off the air", { exact: true }).click();
  await page.getByRole("textbox", { name: "Status note" }).fill("antenna work");
  await page.getByRole("button", { name: "Change status" }).click();
  await saveWithReason("club notice");
  await expect(page.getByText("NOAA WX1 is off-air; links follow")).toBeVisible();
  await expect(page.getByRole("row", { name: /WX1/ })).toContainText("off-air");
});

// A radio CHIRP supports but rflib doesn't: chirp-generic with the radio's
// name length.
test("generic CHIRP radio with a name length", async ({ page }) => {
  await page.goto("/l/default/radios");
  await page.getByRole("button", { name: "Add radio" }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Spare");
  await page.getByRole("combobox", { name: "Model" }).click();
  await page.getByRole("option", { name: /chirp-generic/ }).click();
  await expect(page.getByText("This model doesn't know the radio's limit")).toBeVisible();
  await page.getByRole("textbox", { name: "Name length" }).fill("7");
  await page.getByRole("dialog").getByRole("button", { name: "Add" }).click();
  await expect(page).toHaveURL(/\/radios\/Spare$/);
  await expect(page.getByText("names up to 7")).toBeVisible();
});

// A channel's radius is drawn under its marker and takes no clicks, so the
// marker still opens its popup.
test("map markers stay clickable inside a radius", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/channels", {
    data: { name: "Radius Test", rx_hz: 145_650_000, mode: "FM", location: { latitude: 34.69, longitude: -120.46, radius_km: 50 } },
  });
  await page.goto("/l/default/map");
  await page.getByLabel("Search").fill("name:\"Radius Test\"");
  await expect(page.getByText("1 channels with a location")).toBeVisible();
  // The marker is the smallest shape on the map; click its center.
  const paths = page.locator(".leaflet-overlay-pane path");
  await expect(paths).toHaveCount(2);
  const boxes = await paths.evaluateAll((ps) => ps.map((p) => p.getBoundingClientRect().toJSON()));
  const marker = boxes.reduce((a, b) => (a.width <= b.width ? a : b));
  await page.mouse.click(marker.x + marker.width / 2, marker.y + marker.height / 2);
  await expect(page.locator(".leaflet-popup").getByText("Radius Test")).toBeVisible();
});

// The channel editor suggests tags already in use, and still takes new ones.
test("tags suggest existing ones", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/channels", { data: { name: "Tagged", rx_hz: 146_550_000, mode: "FM", tags: ["fieldday"] } });
  await page.goto("/l/default/channels");
  await page.getByRole("button", { name: "New channel" }).click();
  const tags = page.getByRole("combobox", { name: "Tags" });
  await tags.click();
  await tags.fill("field");
  await page.getByRole("option", { name: "fieldday" }).click();
  await tags.fill("brandnew");
  await tags.press("Enter");
  await expect(page.getByRole("dialog").getByText("fieldday", { exact: true })).toBeVisible();
  await expect(page.getByRole("dialog").getByText("brandnew", { exact: true })).toBeVisible();
});

// A private channel that matches a shared one is offered for linking.
test("link my channels to the shared list", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/channels", {
    data: { name: "My MURS 1", rx_hz: 151_820_000, tx_hz: 151_820_000, mode: "NFM", service: "murs" },
  });
  await page.goto("/l/default/channels");
  await page.getByRole("button", { name: "Link to shared list" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("row", { name: /My MURS 1/ })).toBeVisible();
  await dialog.getByRole("button", { name: /^Link \d+ channels$/ }).click();
  await expect(page.getByText(/Linked \d+ channels to the shared list/)).toBeVisible();
  await page.getByRole("button", { name: "Link to shared list" }).click();
  await expect(page.getByRole("dialog").getByText("None of your channels match a shared channel.")).toBeVisible();
});

// A search becomes a radio source, with a preview of what it would add,
// and the Sources tab previews a new source the same way.
test("add a search as a radio source, with a preview", async ({ page, request }) => {
  const link = (query: string) => request.post("/api/v1/libraries/default/channels/link", { data: { select: { query } } });
  await link("service:gmrs");
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Previewer", model: "baofeng-uv5rm" } });
  await page.goto("/l/default/channels");
  await page.getByLabel("Search").first().fill("service:gmrs");
  await page.getByRole("button", { name: "Add search as source" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox", { name: "Radio" }).click();
  await page.getByRole("option", { name: "Previewer" }).click();
  await expect(dialog.getByText(/Sync would add \d+ channels to Previewer/)).toBeVisible();
  // In the dialog the list scrolls within a set height.
  await expect(dialog.locator(".mantine-ScrollArea-root")).toHaveCount(1);
  await dialog.getByRole("button", { name: "Add source and sync" }).click();
  await expect(page.getByText(/Added the search to Previewer as a source and synced: \d+ channels added/)).toBeVisible();
  // On a radio, they're in use, so the channel list shows them, marked shared.
  await page.getByLabel("Search", { exact: true }).fill('name:"GMRS 15R"');
  const row = page.getByRole("row", { name: /^Select GMRS 15R/ });
  await expect(row).toBeVisible();
  await expect(row.getByLabel("Shared channel")).toBeVisible();

  await page.goto("/l/default/radios/Previewer");
  await page.getByRole("tab", { name: "Sources" }).click();
  await page.getByLabel("New source query", { exact: true }).fill("service:gmrs");
  await expect(page.getByText("Would add nothing new")).toBeVisible();
  // On the page the list takes the room it needs, not a scrolling box.
  await link("service:noaa");
  await page.getByLabel("New source query", { exact: true }).fill("service:noaa");
  const preview = page.getByLabel("Source preview");
  await expect(preview.getByText(/Sync would add 7 channels to Previewer/)).toBeVisible();
  await expect(preview.getByRole("row")).toHaveCount(7);
  await expect(preview.locator(".mantine-ScrollArea-root")).toHaveCount(0);

  // Add it, then edit it with a preview in its place, and move it first.
  await page.getByRole("button", { name: "Add source" }).click();
  await expect(page.getByText("Source added")).toBeVisible();
  await page.getByRole("button", { name: "Edit source 2" }).click();
  const edit = page.getByRole("dialog");
  await edit.getByRole("textbox", { name: "Query" }).fill('service:noaa OR name:"2m FM Call"');
  await expect(edit.getByText(/Sync would add 8 channels to Previewer/)).toBeVisible();
  await edit.getByRole("button", { name: "Save source" }).click();
  await expect(page.getByText("Source saved")).toBeVisible();
  await expect(page.getByRole("row", { name: /2m FM Call/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Move source 1 up" })).toBeDisabled();
  await page.getByRole("button", { name: "Move source 2 up" }).click();
  await expect(page.getByText("Source moved")).toBeVisible();
  await expect(page.getByRole("row").nth(1)).toContainText("2m FM Call");
});

test("compare a radio with its file, and start it from that", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Adopted", model: "baofeng-uv5rm" } });
  await page.goto("/l/default/radios/Adopted");
  await page.getByRole("tab", { name: "Compare" }).click();
  // A CHIRP file of what the radio holds: memories 4 and 9.
  const chooser = page.waitForEvent("filechooser");
  await page.getByLabel("The radio's file", { exact: true }).click();
  await (await chooser).setFiles("e2e/fixtures/adopted-radio.csv");
  await expect(page.getByText("2 only on the radio")).toBeVisible();
  // The first test's Marian repeater programs exactly as memory 9 does, so
  // it's chosen; memory 4 matches nothing and is kept as programmed.
  await expect(page.getByRole("combobox", { name: "Memory 9" })).toHaveValue("Use Marian (exact match)");
  await expect(page.getByRole("combobox", { name: "Memory 4" })).toHaveValue("Keep as programmed");
  await page.getByRole("button", { name: "Apply" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText(/Used 1, kept 1/)).toBeVisible();
  // Compared again: nothing is only on the radio; Marian's name differs.
  await expect(page.getByText("0 only on the radio")).toBeVisible();
  await expect(page.getByText("1 changed")).toBeVisible();
  // Each memory kept its number, each in a section starting there.
  await page.getByRole("tab", { name: "Memories" }).click();
  await expect(page.getByRole("row", { name: /Simplex A/ }).getByRole("cell").nth(2)).toHaveText("4");
  await expect(page.getByRole("row", { name: /Marian/ }).getByRole("cell").nth(2)).toHaveText("9");
});

test("a first import ignores memory numbers", async ({ page, request }) => {
  // rflib already has Marian first on this radio; the radio holds it at 9.
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Imported", model: "baofeng-uv5rm" } });
  await request.post("/api/v1/libraries/default/radios/Imported/channels", { data: { query: "name:Marian" } });
  await page.goto("/l/default/radios/Imported");
  await page.getByRole("tab", { name: "Compare" }).click();
  await page.getByRole("switch", { name: "Ignore memory numbers" }).check({ force: true });
  const chooser = page.waitForEvent("filechooser");
  await page.getByLabel("The radio's file", { exact: true }).click();
  await (await chooser).setFiles("e2e/fixtures/adopted-radio.csv");
  await expect(page.getByText("1 only on the radio")).toBeVisible();
  await expect(page.getByText("0 only in rflib")).toBeVisible();
  await page.getByRole("button", { name: "Apply" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Apply" }).click();
  await expect(page.getByText("0 only on the radio")).toBeVisible();
  // What was missing joined the end; rflib's order stands.
  await page.getByRole("tab", { name: "Memories" }).click();
  await expect(page.getByRole("row", { name: /Marian/ }).getByRole("cell").nth(2)).toHaveText("1");
  await expect(page.getByRole("row", { name: /Simplex A/ }).getByRole("cell").nth(2)).toHaveText("2");
});

test("the default search says what more the shared list has", async ({ page }) => {
  await page.goto("/l/default/channels");
  // Source reads as its default, not as Any.
  await expect(page.getByRole("button", { name: "Source filter" })).toHaveText("Source: in use");
  await page.getByLabel("Search", { exact: true }).fill("aprs");
  const more = page.getByRole("button", { name: /\d+ more in the shared list/ });
  await expect(more).toBeVisible();
  await more.click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue(/in:all/);
  await expect(page.getByRole("row", { name: /APRS/ }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Source filter" })).toHaveText("Source: mine and the shared list");
  await expect(more).toHaveCount(0);
  // Clear starts over: no search, and Source back to its default.
  await page.getByRole("button", { name: "Clear" }).click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("");
  await expect(page.getByRole("button", { name: "Source filter" })).toHaveText("Source: in use");
  await expect(page.getByRole("button", { name: "Clear" })).toHaveCount(0);
});

test("the memories tab shows what is actually programmed", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Reference", model: "baofeng-uv5rm" } });
  await request.post("/api/v1/libraries/default/radios/Reference/channels", { data: { query: 'name:"NOAA WX2" OR name:"NOAA WX3" OR name:"NOAA WX4"' } });
  const found = await (await request.get(`/api/v1/libraries/shared/channels?q=${encodeURIComponent('name:"NOAA WX3"')}`)).json();
  const wx3 = found.channels[0].id as string;
  const status = (s: string) => request.post(`/api/v1/libraries/shared/channels/${wx3}/status`, { data: { status: s, reason: "test" } });
  await status("retired");

  await page.goto("/l/default/radios/Reference");
  await expect(page.getByText(/1 not programmed, 2 receive only/)).toBeVisible();
  // Retired: listed, but not programmed, so it takes no number and the
  // memories after it move up, as on the programmed radio.
  const wx3Row = page.getByRole("row", { name: /NOAA WX3/ });
  await expect(wx3Row.getByText("Not programmed: retired")).toBeVisible();
  await expect(wx3Row.getByRole("cell").nth(2)).toHaveText("–");
  await expect(page.getByRole("row", { name: /NOAA WX4/ }).getByRole("cell").nth(2)).toHaveText("2");
  await expect(page.getByRole("row", { name: /NOAA WX2/ }).getByText("RX only")).toBeVisible();

  // Back in service, it's programmed again.
  await status("active");
  await page.reload();
  await expect(page.getByRole("row", { name: /NOAA WX3/ }).getByText("RX only")).toBeVisible();
  await expect(page.getByRole("row", { name: /NOAA WX4/ }).getByRole("cell").nth(2)).toHaveText("3");
  await expect(page.getByText(/not programmed/)).toHaveCount(0);
});

test("sections: add one, and drag memories and sections", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Sectioned", model: "baofeng-uv5rm" } });
  await request.post("/api/v1/libraries/default/radios/Sectioned/channels", {
    data: { query: 'name:"NOAA WX1" OR name:"NOAA WX2" OR name:"NOAA WX3"', sort: "name" },
  });
  await page.goto("/l/default/radios/Sectioned");
  const number = (name: string) => page.getByRole("row", { name: new RegExp(`^Select memory.*${name}`) }).getByRole("cell").nth(2);
  await expect(number("NOAA WX3")).toHaveText("3");

  // A section at memory 20, reserving 5.
  await page.getByRole("button", { name: "Add section" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).fill("Weather");
  await dialog.getByRole("textbox", { name: "Start" }).fill("20");
  await dialog.getByRole("textbox", { name: "Size" }).fill("5");
  await expect(dialog.getByRole("textbox", { name: "End" })).toHaveValue("24");
  await dialog.getByRole("button", { name: "Add section" }).click();
  await expect(page.getByText("Section added")).toBeVisible();
  const weather = page.locator("tr[data-section='Weather']");
  await expect(weather).toContainText("Memories 20–24");
  await expect(page.getByText("No channels yet: drag memories here")).toBeVisible();

  // Drag WX2 into it, then WX3 before WX2.
  await page.getByLabel("Drag NOAA WX2").dragTo(weather);
  await expect(number("NOAA WX2")).toHaveText("20");
  await page.getByLabel("Drag NOAA WX3").dragTo(page.getByRole("row", { name: /^Select memory.*NOAA WX2/ }), { targetPosition: { x: 40, y: 2 } });
  await expect(number("NOAA WX3")).toHaveText("20");
  await expect(number("NOAA WX2")).toHaveText("21");
  await expect(weather).toContainText("Memories 20–24");

  // While dragging, a bar at the top lists every section, wherever it is.
  const dropOnBar = async (memory: string, section: string) => {
    await page.getByLabel(`Drag ${memory}`).hover();
    await page.mouse.down();
    await page.mouse.move(10, 300, { steps: 5 }); // the drag starts, and the bar shows
    await page.locator(`[data-drop-section='${section}']`).hover();
    await page.mouse.up();
  };
  await dropOnBar("NOAA WX1", "Weather");
  await expect(number("NOAA WX1")).toHaveText("22");
  await expect(page.locator("[data-drop-section]")).toHaveCount(0);
  await dropOnBar("NOAA WX1", "Other");
  await expect(number("NOAA WX1")).toHaveText("25"); // Other follows Weather's 20-24

  // Drag the Other section's handle onto Weather: Other goes first.
  await page.getByLabel("Drag section Other").dragTo(weather);
  await expect(page.locator("tr[data-section]").first()).toHaveAttribute("data-section", "Other");
  await expect(number("NOAA WX1")).toHaveText("1");

  // Other starts at 1; dragging Weather ahead of it can't keep that, so the
  // move happens and Other's start goes, with a note.
  await request.patch("/api/v1/libraries/default/radios/Sectioned/sections/Other", { data: { start: 1 } });
  await page.reload();
  await page.getByLabel("Drag section Weather").dragTo(page.locator("tr[data-section='Other']"));
  await expect(page.getByText(/these now follow the section before them: Other/)).toBeVisible();
  await expect(page.locator("tr[data-section]").first()).toHaveAttribute("data-section", "Weather");
  await expect(number("NOAA WX1")).toHaveText("25");

  // Several selected memories move to a section at once.
  await page.getByRole("checkbox", { name: /Select memory .*NOAA WX2|Select memory 20/ }).first().click();
  await page.getByRole("button", { name: "Move to section" }).click();
  await page.getByRole("menuitem", { name: "Other" }).click();
  await expect(page.getByText("Moved 1 memories")).toBeVisible();

  // Dragging a section shows a merge bar; dropping there merges, after a check.
  await page.getByLabel("Drag section Weather").hover();
  await page.mouse.down();
  await page.mouse.move(10, 300, { steps: 5 });
  await page.locator("[data-merge-section='Other']").hover();
  await page.mouse.up();
  await expect(page.getByText("Merge Weather into Other?")).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "Merge" }).click();
  await expect(page.getByText(/Merged: \d+ channels moved/)).toBeVisible();
  await expect(page.locator("tr[data-section]")).toHaveCount(1);
});

test("copy a radio to another model, then compare the two", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Original", model: "chirp-generic" } });
  await request.post("/api/v1/libraries/default/radios/Original/sections", { data: { name: "Weather", start: 40 } });
  await request.post("/api/v1/libraries/default/radios/Original/channels", { data: { query: 'name:"NOAA WX1" OR name:"NOAA WX2"', section: "Weather" } });
  await request.post("/api/v1/libraries/default/radios/Original/channels", { data: { query: 'name:"6m FM Call"' } });
  await page.goto("/l/default/radios/Original");
  await page.getByRole("button", { name: "Copy", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).fill("Twin");
  await dialog.getByRole("combobox", { name: "Model" }).fill("UV-5RM");
  await page.getByRole("option", { name: /baofeng-uv5rm/ }).click();
  await dialog.getByRole("button", { name: "Copy" }).click();
  // The copy opens, with the original's section; 6 m is out of its bands.
  await expect(page).toHaveURL(/\/radios\/Twin$/);
  await expect(page.getByText("Copied Original to Twin: 2 channels")).toBeVisible();
  await expect(page.locator("tr[data-section='Weather']")).toContainText("Memories 40–41");

  // Independent: the original gains a channel; comparing offers it.
  await request.post("/api/v1/libraries/default/radios/Original/channels", { data: { query: 'name:"NOAA WX3"', section: "Weather" } });
  await page.getByRole("tab", { name: "Compare" }).click();
  await page.getByText("With another radio").click();
  await page.getByRole("combobox", { name: "Other radio" }).click();
  await page.getByRole("option", { name: "Original" }).click();
  await expect(page.getByText("2 on both")).toBeVisible();
  await expect(page.getByRole("checkbox", { name: "Add NOAA WX3" })).toBeChecked();
  await expect(page.getByRole("checkbox", { name: "Add 6m FM Call" })).toBeDisabled();
  await page.getByRole("button", { name: "Add 1 channels" }).click();
  await expect(page.getByText("Added 1 channels from Original")).toBeVisible();
  await expect(page.getByText("3 on both")).toBeVisible();

  // Onto a blank radio, the sections the channels need come along.
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Blank", model: "baofeng-uv5rm" } });
  await page.goto("/l/default/radios/Blank");
  await page.getByRole("tab", { name: "Compare" }).click();
  await page.getByText("With another radio").click();
  await page.getByRole("combobox", { name: "Other radio" }).click();
  await page.getByRole("option", { name: "Original" }).click();
  await page.getByRole("button", { name: "Add 3 channels" }).click();
  await expect(page.getByText("Added 3 channels from Original, in new sections Weather")).toBeVisible();
  await page.getByRole("tab", { name: "Memories" }).click();
  await expect(page.locator("tr[data-section='Weather']")).toContainText("Memories 40–42");
});


test("narrowing a source takes off what it no longer matches", async ({ page, request }) => {
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Narrow", model: "baofeng-uv5rm" } });
  await request.post("/api/v1/libraries/default/radios/Narrow/sources", { data: { query: 'name:"NOAA WX1" OR name:"NOAA WX2" OR name:"NOAA WX3"' } });
  await request.post("/api/v1/libraries/default/radios/Narrow/sync", { data: {} });
  await page.goto("/l/default/radios/Narrow");
  const row = (name: string) => page.getByRole("row", { name: new RegExp(`^Select memory.*${name}`) });
  await expect(row("NOAA WX3").getByText("S1")).toBeVisible();

  // Keep WX3: it stays even when the source stops matching it.
  await row("NOAA WX3").getByRole("button", { name: /^Move / }).click();
  await page.getByRole("menuitem", { name: "Keep, even if source 1 stops matching" }).click();
  await expect(page.getByText("Kept 1 memories")).toBeVisible();
  await expect(row("NOAA WX3").getByText("S1")).toHaveCount(0);

  // Narrow the source to WX1; sync takes WX2 off.
  await request.patch("/api/v1/libraries/default/radios/Narrow/sources/1", { data: { query: 'name:"NOAA WX1"' } });
  await page.getByRole("button", { name: "Sync" }).click();
  await expect(page.getByText(/0 to add, 1 to take off/)).toBeVisible();
  await page.getByRole("button", { name: "Sync now" }).click();
  await expect(row("NOAA WX2")).toHaveCount(0);
  await expect(row("NOAA WX3")).toBeVisible();

  // Select all in a section, for the bulk actions.
  const header = page.locator("tr[data-section]").first();
  await header.getByRole("button", { name: /^Select all in/ }).click();
  await expect(page.getByText("2 selected")).toBeVisible();
  await page.getByRole("button", { name: "Clear", exact: true }).first().click();

  // Removing the source's section takes the source with it.
  await header.getByRole("button", { name: /^Edit section/ }).click();
  await page.getByRole("button", { name: "Remove section" }).click();
  await expect(page.getByText(/The source that feeds it goes too|that feed it go too/)).toBeVisible();
  await page.getByRole("checkbox", { name: /Also take its channels off/ }).check();
  await page.getByRole("button", { name: "Remove", exact: true }).click();
  await expect(page.getByText(/Section removed, with its 2 channels, and the source that fed it/)).toBeVisible();
  await page.getByRole("tab", { name: "Sources" }).click();
  await expect(page.getByRole("cell", { name: /NOAA WX1/ })).toHaveCount(0);
});
