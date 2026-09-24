import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const infraRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const compose = readFileSync(resolve(infraRoot, "infra/compose/compose.yaml"), "utf8");
const garageConfig = readFileSync(resolve(infraRoot, "infra/compose/garage.toml"), "utf8");
const runner = readFileSync(resolve(infraRoot, "scripts/infra/infra.mjs"), "utf8");

test("Garage RPC secret is never tracked and is injected from a persistent file", () => {
  assert.doesNotMatch(garageConfig, /^\s*rpc_secret\s*=/m);
  assert.match(compose, /GARAGE_RPC_SECRET_FILE:/);
  assert.match(compose, /garage-rpc-secret:\/run\/secrets:ro/);
  assert.match(compose, /garage-secret-init:/);
  assert.match(compose, /busybox:1\.37\.0@sha256:/);
  assert.match(compose, /od -An -N32 -tx1 \/dev\/urandom/);
});

test("concurrent smoke runs get separate projects and dynamically assigned ports", () => {
  assert.match(runner, /randomBytes\(8\)/);
  assert.match(runner, /const testProject = `atrisk-test-\$\{runToken\}`/);
  assert.match(runner, /testProject\.length > 63/);
  assert.match(runner, /POSTGRES_PORT: ""/);
  assert.match(runner, /GARAGE_S3_PORT: ""/);
});

test("smoke cleanup handles signals, normal completion, and process exit", () => {
  assert.match(runner, /process\.once\("SIGINT", handleSignal\)/);
  assert.match(runner, /process\.once\("SIGTERM", handleSignal\)/);
  assert.match(runner, /process\.once\("exit"/);
  assert.match(runner, /down", "--volumes", "--remove-orphans/);
  assert.match(runner, /cleanupDone/);
});
