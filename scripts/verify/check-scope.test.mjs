import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const run = promisify(execFile);
const checker = resolve(import.meta.dirname, "check-scope.mjs");
const fixtures = new Set();

afterEach(async () => {
  await Promise.all([...fixtures].map((directory) => rm(directory, { recursive: true, force: true })));
  fixtures.clear();
});

async function runChecker(label, root) {
  try {
    const result = await run("node", [checker, label, root], {
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

test("passes when a scope has no sources", async () => {
  const directory = await mkdtemp(join(tmpdir(), "atrisk-scope-"));
  fixtures.add(directory);
  const result = await runChecker("integration", join(directory, "test", "integration"));
  assert.equal(result.code, 0);
  assert.match(result.stdout, /no integration sources/i);
});

test("fails closed when a scope source appears without a runner", async () => {
  const directory = await mkdtemp(join(tmpdir(), "atrisk-scope-"));
  fixtures.add(directory);
  const source = join(directory, "test", "integration");
  await writeFile(join(directory, "marker.txt"), "fixture\n");
  await import("node:fs/promises").then(({ mkdir }) => mkdir(source, { recursive: true }));
  await writeFile(join(source, "example.txt"), "source\n");

  const result = await runChecker("integration", source);
  assert.notEqual(result.code, 0);
  assert.match(result.stderr, /sources exist but this foundation target has no runner/i);
});
