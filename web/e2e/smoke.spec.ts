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

  // Link NOAA weather radio from the shared list: seven links appear.
  await page.getByRole("link", { name: "Channels", exact: true }).click();
  await page.getByRole("switch", { name: "Search the shared list" }).click();
  await page.getByLabel("Search", { exact: true }).fill("service:noaa");
  await expect(page.getByText("7 channels")).toBeVisible();
  await page.getByRole("checkbox", { name: "Select all" }).click();
  await page.getByRole("button", { name: "Link into my library" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Link 7 channels" }).click();
  await expect(page.getByText("Linked 7 channels")).toBeVisible();
  await page.getByRole("switch", { name: "Search the shared list" }).click();
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
  await page.getByRole("button", { name: "Add channel" }).click();
  await expect(page.getByText("Channel added")).toBeVisible();
  const row = page.getByRole("row", { name: /Marian/ });
  await expect(row).toContainText("147.300000");
  await expect(row).toContainText("T131.8");

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
  await expect(page.getByText("1 of them are linked")).toBeVisible();
  await page.getByRole("button", { name: "Set on 1 channels" }).click();
  await expect(page.getByText("Set the service on 1 channels")).toBeVisible();
  await expect(page.getByRole("row", { name: /Marian/ })).toContainText("amateur");

  // A linked channel's shared fields can't be edited.
  await page.getByRole("row", { name: /WX1/ }).getByText(/WX1/).click();
  await expect(page.getByRole("dialog").getByText("Linked to a shared channel")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Receive MHz" })).toBeDisabled();
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
  await expect(page.getByRole("row", { name: /Marian/ })).toBeVisible();

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

  // The plan on a radio, from memory 50.
  await request.post("/api/v1/libraries/default/radios", { data: { name: "Mobile", model: "baofeng-uv5rm" } });
  await page.goto("/l/default/radios/Mobile");
  await page.getByRole("tab", { name: "Sources" }).click();
  await page.getByRole("combobox", { name: "or a plan" }).click();
  await page.getByRole("option", { name: "Fair" }).click();
  await page.getByRole("textbox", { name: "Section start" }).fill("50");
  await page.getByRole("button", { name: "Add source" }).click();
  await expect(page.getByText("Plan: Fair")).toBeVisible();
  await page.getByRole("button", { name: "Sync" }).click();
  await expect(page.getByText(/plan Fair\): 2 to add/)).toBeVisible();
  await page.getByRole("button", { name: "Sync now" }).click();
  await page.getByRole("tab", { name: "Memories" }).click();
  await expect(page.getByRole("row", { name: /FAIR TAC1/ })).toContainText("50");
  await expect(page.getByRole("row", { name: /70cm FM Call/ })).toContainText("59");
});

// Runs after the first test: from the shared list, link a channel onto a
// radio, then correct it as the maintainer.
test("the shared list: link, correct, and take off the air", async ({ page }) => {
  const saveWithReason = async (reason: string) => {
    await page.getByRole("textbox", { name: "Reason" }).fill(reason);
    await page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click();
  };
  await page.goto("/l/shared/channels");
  await page.getByLabel("Search", { exact: true }).fill("name:wx1");
  await expect(page.getByText("1 channels")).toBeVisible();

  // Already linked, so linking only puts its link on the radio.
  await page.getByRole("checkbox", { name: "Select NOAA WX1" }).click();
  await page.getByRole("button", { name: "Link into my library" }).click();
  await page.getByRole("combobox", { name: "Also add to a radio" }).click();
  await page.getByRole("option", { name: "HT" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Link 1 channels" }).click();
  await expect(page.getByText("Linked 0 channels; 1 already linked. Added 1 to HT")).toBeVisible();

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
});
