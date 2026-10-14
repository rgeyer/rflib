// Starts rflib serve on a fresh database for the smoke tests. rflib must be
// built at the repository root first (make e2e does that).
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const rflib = join(import.meta.dirname, "..", "..", "rflib");
const db = join(mkdtempSync(join(tmpdir(), "rflib-e2e-")), "e2e.db");
execFileSync(rflib, ["--db", db, "init"], { stdio: "inherit" });
const server = spawn(rflib, ["--db", db, "serve", "--addr", "127.0.0.1:18765"], { stdio: "inherit" });
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => server.kill(sig));
server.on("exit", (code) => process.exit(code ?? 0));
