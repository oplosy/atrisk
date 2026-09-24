import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const run = promisify(execFile);
const repoRoot = resolve(import.meta.dirname, "../..");
const checker = resolve(repoRoot, "scripts/verify/check-generated.mjs");
const fixtures = new Set();

afterEach(async () => {
  await Promise.all([...fixtures].map((directory) => rm(directory, { recursive: true, force: true })));
  fixtures.clear();
});

async function git(directory, args) {
  return run("git", args, { cwd: directory, encoding: "utf8" });
}

async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), "atrisk-generated-"));
  fixtures.add(directory);
  await git(directory, ["init", "-q"]);
  await git(directory, ["config", "user.email", "verify@example.invalid"]);
  await git(directory, ["config", "user.name", "AtlasRisk Verification"]);
  await writeFile(join(directory, "source.txt"), "source\n");
  await git(directory, ["add", "source.txt"]);
  await git(directory, ["commit", "-qm", "fixture"]);
  return directory;
}

async function runChecker(directory) {
  try {
    const result = await run("node", [checker], {
      cwd: directory,
      encoding: "utf8",
      maxBuffer: 1024 * 1024,
    });
    return { code: 0, ...result };
  } catch (error) {
    return {
      code: error.code ?? 1,
      stdout: error.stdout ?? "",
      stderr: error.stderr ?? "",
    };
  }
}

test("passes a clean checkout with no generated artifacts", async () => {
  const directory = await fixture();
  const result = await runChecker(directory);
  assert.equal(result.code, 0);
  assert.match(result.stdout, /no generated artifacts are present/i);
});

test("fails on modified tracked generated output", async () => {
  const directory = await fixture();
  await writeFile(join(directory, "generated.go"), "package generated\n");
  await git(directory, ["add", "generated.go"]);
  await git(directory, ["commit", "-qm", "generated fixture"]);
  await writeFile(join(directory, "generated.go"), "package changed\n");

  const result = await runChecker(directory);
  assert.notEqual(result.code, 0);
  assert.match(result.stderr, /Generated-file drift detected/);
});

test("fails on deleted tracked and untracked generated output", async (t) => {
  await t.test("deleted tracked output", async () => {
    const directory = await fixture();
    await writeFile(join(directory, "schema_gen.go"), "package generated\n");
    await git(directory, ["add", "schema_gen.go"]);
    await git(directory, ["commit", "-qm", "generated fixture"]);
    await rm(join(directory, "schema_gen.go"));

    const result = await runChecker(directory);
    assert.notEqual(result.code, 0);
    assert.match(result.stderr, /schema_gen\.go/);
  });

  await t.test("untracked output", async () => {
    const directory = await fixture();
    await writeFile(join(directory, "gen.go"), "package generated\n");

    const result = await runChecker(directory);
    assert.notEqual(result.code, 0);
    assert.match(result.stderr, /gen\.go/);
  });
});
