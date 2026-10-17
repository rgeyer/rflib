import { expect, test } from "@playwright/test";

// A server with accounts (serve.mjs starts it, with the admin N0CALL and
// open sign-up): sign-in comes first, each account has its own library,
// and members don't get the maintainer's tools.
const ACCOUNTS = "http://127.0.0.1:18766";
test.use({ baseURL: ACCOUNTS });

test.beforeAll(async ({ request }) => {
  for (let i = 0; i < 50; i++) {
    const ok = await request
      .get(`${ACCOUNTS}/api/v1/health`)
      .then((r) => r.ok())
      .catch(() => false);
    if (ok) return;
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error("the server with accounts didn't start");
});

test("sign in, register, and each account has its own library", async ({ page, browser }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
  await page.getByLabel("Callsign or username").fill("n0call");
  await page.getByLabel(/^Password\s*\*?$/).fill("wrong password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("The name or password is wrong")).toBeVisible();
  await page.getByLabel(/^Password\s*\*?$/).fill("admin password");
  await page.getByRole("button", { name: "Sign in" }).click();

  // The admin has the library from before accounts, and the maintainer's
  // tools.
  await expect(page).toHaveURL(/\/l\/default\/channels$/);
  await expect(page.getByRole("button", { name: "Account" })).toContainText("N0CALL");
  await expect(page.getByRole("link", { name: /Built-in lists/ })).toBeVisible();

  // Someone else registers in another browser, and gets a library of
  // their own, without the maintainer's tools.
  const other = await browser.newContext({ baseURL: ACCOUNTS });
  const theirs = await other.newPage();
  await theirs.goto("/");
  await theirs.getByRole("button", { name: "Make an account" }).click();
  await theirs.getByLabel("Callsign or username").fill("N1CALL");
  await theirs.getByLabel(/^Password\s*\*?$/).fill("a long password");
  await theirs.getByLabel("Password again").fill("a long password");
  await theirs.getByLabel("Display name").fill("Pat");
  await theirs.getByRole("button", { name: "Make the account" }).click();
  await expect(theirs).toHaveURL(/\/l\/n1call\/channels$/);
  await expect(theirs.getByRole("button", { name: "Account" })).toContainText("Pat");
  await expect(theirs.getByRole("link", { name: /Built-in lists/ })).toHaveCount(0);
  await theirs.getByRole("link", { name: "Organizations" }).click();
  await expect(theirs.getByRole("heading", { name: "Organizations" })).toBeVisible();
  await expect(theirs.getByRole("button", { name: "New organization" })).toHaveCount(0);

  // The shared list is theirs to read: the new database's built-in lists.
  await theirs.getByRole("link", { name: "Channels", exact: true }).click();
  await theirs.getByLabel("Search", { exact: true }).fill("service:noaa");
  await theirs.getByRole("button", { name: "Source filter" }).click();
  await theirs.getByRole("menuitem", { name: "the shared list", exact: true }).click();
  await expect(theirs.getByText("7 channels")).toBeVisible();

  // The admin's library isn't theirs to open.
  await theirs.goto("/l/default/channels");
  await expect(theirs.getByText(/not found|no library/i).first()).toBeVisible();

  // Signing out goes back to sign-in.
  await theirs.getByRole("button", { name: "Account" }).click();
  await theirs.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(theirs.getByRole("button", { name: "Sign in" })).toBeVisible();
  await other.close();
});
