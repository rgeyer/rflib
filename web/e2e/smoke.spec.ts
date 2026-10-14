import { expect, test } from "@playwright/test";

// One pass through the main screens against a real rflib serve: install a
// built-in list, add and edit a channel, set up a radio, sync it, and
// export its file.
test("lists, channels, radios, and export", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/l\/default\/channels$/);
  await expect(page.getByText("0 channels")).toBeVisible();

  // Install NOAA weather radio: seven links appear.
  await page.getByRole("link", { name: "Built-in lists" }).click();
  const noaa = page.locator(".mantine-Card-root", { hasText: "us-noaa" });
  await noaa.getByRole("button", { name: "Install" }).click();
  await expect(page.getByText("Links 7 new channels")).toBeVisible();
  await page.getByRole("dialog").getByRole("button", { name: "Install" }).click();
  await expect(page.getByText("us-noaa: linked 7")).toBeVisible();

  // Add a repeater with a tone.
  await page.getByRole("link", { name: "Channels" }).click();
  await expect(page.getByText("7 channels")).toBeVisible();
  await page.getByRole("button", { name: "New channel" }).click();
  await page.getByRole("textbox", { name: "Name" }).fill("Marian");
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

  // Search narrows the table.
  await page.getByLabel("Search", { exact: true }).fill("band:noaa");
  await expect(page.getByText("7 channels")).toBeVisible();
  await page.getByLabel("Search", { exact: true }).fill("");

  // A linked channel's shared fields can't be edited.
  await page.getByRole("row", { name: /WX1/ }).getByText(/WX1/).click();
  await expect(page.getByRole("dialog").getByText("Linked to a shared channel")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Receive MHz" })).toBeDisabled();
  await page.keyboard.press("Escape");

  // A radio with a source; sync adds the matches.
  await page.getByRole("link", { name: "Radios" }).click();
  await page.getByRole("button", { name: "Add radio" }).click();
  await page.getByRole("textbox", { name: "Name" }).fill("HT");
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
  await page.getByRole("button", { name: "Export" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe("HT.csv");
  await expect(page.getByText("Exported 8 memories")).toBeVisible();
});

test("the map shows located channels", async ({ page }) => {
  await page.goto("/l/default/map");
  await expect(page.getByRole("heading", { name: "Map" })).toBeVisible();
  await expect(page.locator(".leaflet-container")).toBeVisible();
});
