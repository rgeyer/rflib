// Empties dist/ for a new build but keeps .gitkeep, which lets Go embed the
// directory before the interface has been built.
import { readdirSync, rmSync } from "node:fs";
for (const name of readdirSync("dist")) {
  if (name !== ".gitkeep") rmSync(`dist/${name}`, { recursive: true, force: true });
}
