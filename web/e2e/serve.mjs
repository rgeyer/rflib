// Starts rflib serve on fresh databases for the smoke tests: one without
// accounts, and one with them. rflib must be
// built at the repository root first (make e2e does that).
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const rflib = join(import.meta.dirname, "..", "..", "rflib");
const db = join(mkdtempSync(join(tmpdir(), "rflib-e2e-")), "e2e.db");
execFileSync(rflib, ["--db", db, "init"], { stdio: "inherit" });
// A second server has accounts, with an admin and open sign-up.
const accountsDB = join(mkdtempSync(join(tmpdir(), "rflib-e2e-")), "accounts.db");
execFileSync(rflib, ["--db", accountsDB, "init"], { stdio: "inherit" });
execFileSync(rflib, ["--db", accountsDB, "user", "add", "N0CALL", "--admin"], { input: "admin password\n", stdio: ["pipe", "inherit", "inherit"] });
const servers = [
  spawn(rflib, ["--db", db, "serve", "--addr", "127.0.0.1:18765"], { stdio: "inherit" }),
  spawn(rflib, ["--db", accountsDB, "serve", "--addr", "127.0.0.1:18766", "--accounts", "--signup", "open"], { stdio: "inherit" }),
];
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => servers.forEach((s) => s.kill(sig)));
for (const s of servers) s.on("exit", (code) => process.exit(code ?? 0));
