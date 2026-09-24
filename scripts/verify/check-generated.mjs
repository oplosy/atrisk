import { readFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { resolve, sep } from "node:path";

const run = promisify(execFile);
const repoRoot = process.cwd();
const generatorManifest = resolve(repoRoot, "scripts/verify/generators.json");

const generatedPath = (file) => {
  const normalized = file.replaceAll("\\", "/");
  if (normalized.startsWith("scripts/verify/")) return false;
  const parts = normalized.split("/");
  const basename = parts.at(-1) ?? "";
  const generatedDirectory = parts.slice(0, -1).some((part) =>
    /^(generated|gen|autogen|__generated__)$/i.test(part),
  );
  const generatedBasename =
    /^(?:generated|gen|mock_|zz_generated)/i.test(basename) ||
    /(?:^|[._-])(?:generated|gen|pb|mock)(?:[._-]|$)/i.test(basename);
  return generatedDirectory || generatedBasename;
};

const splitNull = (value) => value.split("\0").filter(Boolean);

async function gitFiles(args) {
  const { stdout } = await run("git", args, { cwd: repoRoot, encoding: "utf8" });
  return splitNull(stdout).map((file) => file.replaceAll("\\", "/"));
}

async function currentGeneratedChanges(includeCleanTracked) {
  const [tracked, changed, status] = await Promise.all([
    gitFiles(["ls-files", "-z"]),
    gitFiles(["diff", "HEAD", "--name-only", "--no-renames", "-z", "--"]),
    gitFiles(["status", "--short", "--untracked-files=all", "-z"]),
  ]);
  const statusPaths = status.map((entry) => entry.slice(3));
  const candidates = [
    ...changed,
    ...statusPaths,
    ...(includeCleanTracked ? tracked : []),
  ];
  return [
    ...new Set(
      candidates.filter((file) => generatedPath(file)),
    ),
  ].sort();
}

async function loadGenerators() {
  try {
    const raw = await readFile(generatorManifest, "utf8");
    const config = JSON.parse(raw);
    if (!Array.isArray(config.generators)) {
      throw new Error("generators must be an array");
    }
    return config.generators;
  } catch (error) {
    if (error.code === "ENOENT") return [];
    throw new Error("invalid " + generatorManifest + ": " + error.message);
  }
}

async function runGenerators(generators) {
  for (const generator of generators) {
    if (
      !generator ||
      typeof generator.command !== "string" ||
      !Array.isArray(generator.args) ||
      (generator.cwd !== undefined && typeof generator.cwd !== "string")
    ) {
      throw new Error(
        "each generator must define string command, array args, and optional string cwd",
      );
    }
    const cwd = resolve(repoRoot, generator.cwd ?? ".");
    if (!cwd.startsWith(repoRoot + sep) && cwd !== repoRoot) {
      throw new Error("generator cwd escapes repository: " + generator.cwd);
    }
    console.log("Running generator: " + (generator.name ?? generator.command));
    await run(generator.command, generator.args, {
      cwd,
      encoding: "utf8",
      maxBuffer: 10 * 1024 * 1024,
      stdio: "inherit",
    });
  }
}

try {
  const generators = await loadGenerators();
  const before = await currentGeneratedChanges(generators.length === 0);

  if (generators.length > 0) await runGenerators(generators);

  const after = await currentGeneratedChanges(generators.length === 0);
  if (after.length > 0) {
    console.error("Generated-file drift detected:");
    for (const file of after) console.error("- " + file);
    if (generators.length === 0) {
      console.error(
        "Generated artifacts exist but scripts/verify/generators.json has no regeneration commands.",
      );
    } else if (before.length === 0) {
      console.error(
        "A registered generator changed the checkout; commit its deterministic output.",
      );
    } else {
      console.error(
        "Regeneration did not leave generated artifacts identical to HEAD.",
      );
    }
    process.exitCode = 1;
  } else if (generators.length === 0) {
    console.log(
      "Generated-file drift check passed: no generated artifacts are present and no generator is registered yet.",
    );
    console.log(
      "AR-005 must add generator commands to scripts/verify/generators.json before introducing generated outputs.",
    );
  } else {
    console.log("Generated-file drift check passed after registered regeneration.");
  }
} catch (error) {
  console.error("Generated-file drift check failed closed: " + error.message);
  process.exitCode = 1;
}
