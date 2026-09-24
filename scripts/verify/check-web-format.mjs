import { execFileSync } from "node:child_process";

const prettier = process.platform === "win32" ? ".\\node_modules\\.bin\\prettier.cmd" : "node_modules/.bin/prettier";
const endOfLine = process.platform === "win32" ? "crlf" : "lf";

execFileSync(prettier, ["--check", "apps/web", "--end-of-line", endOfLine], {
  stdio: "inherit",
  shell: process.platform === "win32",
});
