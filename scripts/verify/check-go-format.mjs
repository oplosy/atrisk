import { readdir, readFile } from "node:fs/promises";
import { spawn } from "node:child_process";

function runGofmt(source) {
  return new Promise((resolve, reject) => {
    const child = spawn("gofmt", [], { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.once("error", reject);
    child.once("close", (status) => resolve({ status, stdout, stderr }));
    child.stdin.end(source);
  });
}

async function goFiles(root) {
  const entries = await readdir(root, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const path = `${root}/${entry.name}`;
    if (entry.isDirectory()) files.push(...(await goFiles(path)));
    else if (entry.isFile() && entry.name.endsWith(".go")) files.push(path);
  }

  return files;
}

const files = [];
for (const root of ["apps", "internal"]) files.push(...(await goFiles(root)));

const failures = [];
for (const file of files.sort()) {
  const source = (await readFile(file, "utf8")).replaceAll("\r\n", "\n");
  const result = await runGofmt(source);
  if (result.status !== 0) {
    failures.push(`${file}: gofmt failed (${result.stderr.trim()})`);
    continue;
  }
  if (result.stdout !== source) failures.push(file);
}

if (failures.length > 0) {
  console.error("Go format check failed:");
  for (const failure of failures) console.error(`- ${failure}`);
  process.exitCode = 1;
} else {
  console.log(`Go format check passed (${files.length} files).`);
}
