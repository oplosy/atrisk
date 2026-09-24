import { execFileSync } from "node:child_process";

const prettier = process.platform === "win32" ? ".\\node_modules\\.bin\\prettier.cmd" : "node_modules/.bin/prettier";
const endOfLine = process.platform === "win32" ? "crlf" : "lf";
const command = process.platform === "win32" ? process.env.ComSpec : prettier;
const args =
  process.platform === "win32"
    ? ["/d", "/s", "/c", prettier + " --check apps/web --end-of-line " + endOfLine]
    : ["--check", "apps/web", "--end-of-line", endOfLine];

execFileSync(command, args, { stdio: "inherit" });
