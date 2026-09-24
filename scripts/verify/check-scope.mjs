import { access, readdir } from "node:fs/promises";

const [, , label, ...roots] = process.argv;
if (!label || roots.length === 0) {
  console.error("usage: node check-scope.mjs <label> <path> [...]");
  process.exit(2);
}

async function hasFiles(root) {
  try {
    await access(root);
  } catch {
    return false;
  }

  const entries = await readdir(root, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isFile()) return true;
    if (entry.isDirectory() && (await hasFiles(`${root}/${entry.name}`))) return true;
  }
  return false;
}

const existing = [];
for (const root of roots) if (await hasFiles(root)) existing.push(root);

if (existing.length > 0) {
  console.error(`${label} sources exist but this foundation target has no runner: ${existing.join(", ")}`);
  console.error("Update the task with the owning packet before adding sources.");
  process.exitCode = 1;
} else {
  console.log(`${label} check passed (no ${label} sources are present yet).`);
}
