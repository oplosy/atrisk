import { execFile } from "node:child_process";
import { promisify } from "node:util";

const run = promisify(execFile);
const generatedPath = /(^|\/)(generated|gen)(\/|$)|\.(gen|generated)\./i;

async function changedFiles(args) {
  const { stdout } = await run("git", args, { encoding: "utf8" });
  return stdout.split(/\r?\n/).filter(Boolean).map((file) => file.replaceAll("\\", "/"));
}

const trackedChanges = [
  ...(await changedFiles(["diff", "--name-only", "--diff-filter=ACMRTUXB"])),
  ...(await changedFiles(["diff", "--cached", "--name-only", "--diff-filter=ACMRTUXB"])),
];
const status = (await run("git", ["status", "--short", "--untracked-files=all"], { encoding: "utf8" })).stdout;
const untrackedChanges = status
  .split(/\r?\n/)
  .filter(Boolean)
  .map((line) => line.slice(3).replaceAll("\\", "/"));
const drift = [...new Set([...trackedChanges, ...untrackedChanges].filter((file) => generatedPath.test(file)))];

if (drift.length > 0) {
  console.error("Generated-file drift detected:");
  for (const file of drift) console.error(`- ${file}`);
  console.error("Regenerate the artifact and commit the result, or remove the stale generated file.");
  process.exitCode = 1;
} else {
  console.log("Generated-file drift check passed (no generated paths changed).");
}
