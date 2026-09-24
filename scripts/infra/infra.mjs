import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const composeFile = resolve(repoRoot, "infra/compose/compose.yaml");
const projectName = process.env.COMPOSE_PROJECT_NAME || "atrisk";
const composeBase = ["compose", "-f", composeFile, "--project-name", projectName];

function run(args, environment = process.env, capture = false) {
  const result = spawnSync("docker", [...composeBase, ...args], {
    cwd: repoRoot,
    env: environment,
    encoding: "utf8",
    stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    if (capture && result.stderr) process.stderr.write(result.stderr);
    throw new Error(`docker compose exited with status ${result.status}`);
  }
  return capture ? result.stdout : "";
}

function usage() {
  console.error("Usage: node scripts/infra/infra.mjs <start|stop|status|logs|reset|test> [service]");
  console.error("  reset requires the explicit --confirm-reset flag");
  process.exitCode = 2;
}

const [command, option] = process.argv.slice(2);
if (!command) {
  usage();
} else if (command === "start") {
  run(["up", "-d", "--wait"]);
} else if (command === "stop") {
  run(["down"]);
} else if (command === "status") {
  run(["ps"]);
} else if (command === "logs") {
  run(["logs", "--tail=100", ...(option ? [option] : [])]);
} else if (command === "reset") {
  if (option !== "--confirm-reset") {
    console.error("Refusing destructive reset. Re-run with --confirm-reset to remove containers and named volumes.");
    process.exitCode = 2;
  } else {
    console.warn("DESTRUCTIVE: removing the local AtlasRisk containers and named volumes.");
    run(["down", "--volumes", "--remove-orphans"]);
  }
} else if (command === "test") {
  const smokeDir = mkdtempSync(resolve(tmpdir(), "atrisk-infra-smoke-"));
  const testEnvironment = {
    ...process.env,
    COMPOSE_PROJECT_NAME: "atrisk-test",
    POSTGRES_DB: "atrisk_test",
    POSTGRES_USER: "atrisk_test",
    POSTGRES_PASSWORD: "atlasrisk-test-only",
    POSTGRES_PORT: "55433",
    GARAGE_ACCESS_KEY: "GKatrisktest",
    GARAGE_SECRET_KEY: "atlasrisk-garage-test-only-secret",
    GARAGE_BUCKET: "atrisk-test-raw",
    GARAGE_S3_PORT: "53901",
    SMOKE_OUTPUT_DIR: smokeDir,
  };
  const testBase = ["compose", "-f", composeFile, "--project-name", "atrisk-test"];
  const runTest = (args, capture = false) => {
    const result = spawnSync("docker", [...testBase, "--profile", "test", ...args], {
      cwd: repoRoot,
      env: testEnvironment,
      encoding: "utf8",
      stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit",
    });
    if (result.error) throw result.error;
    if (result.status !== 0) {
      if (capture && result.stderr) process.stderr.write(result.stderr);
      throw new Error(`docker compose test command exited with status ${result.status}`);
    }
    return capture ? result.stdout : "";
  };
  const smoke = (args) => runTest(["run", "--rm", "--no-deps", "s3-smoke", ...args], true);
  const endpoint = ["--endpoint-url", "http://garage:3900"];
  const bucket = testEnvironment.GARAGE_BUCKET;
  const key = "smoke/put-get-head-list.txt";
  const payload = readFileSync(resolve(repoRoot, "infra/compose/smoke-payload.txt"));
  const outputFile = resolve(smokeDir, "received.txt");
  let failure;
  try {
    runTest(["up", "-d", "--wait", "postgres", "garage"]);
    runTest(["ps"]);
    smoke(["s3api", "put-object", "--bucket", bucket, "--key", key, "--body", "/aws/smoke-payload.txt", ...endpoint]);
    smoke(["s3api", "get-object", "--bucket", bucket, "--key", key, "/aws/smoke-output/received.txt", ...endpoint]);
    const received = readFileSync(outputFile);
    if (!received.equals(payload)) throw new Error("S3 Get body did not match the Put body");
    const head = JSON.parse(smoke(["s3api", "head-object", "--bucket", bucket, "--key", key, ...endpoint]));
    if (head.ContentLength !== payload.length) throw new Error("S3 Head ContentLength did not match the Put body");
    const listed = smoke(["s3api", "list-objects-v2", "--bucket", bucket, "--prefix", "smoke/", "--query", "Contents[?Key==`smoke/put-get-head-list.txt`].Key", "--output", "text", ...endpoint]).trim();
    if (listed !== key) throw new Error(`S3 List did not return ${key}; received ${JSON.stringify(listed)}`);
    console.log("S3 smoke passed: PutObject, GetObject, HeadObject, ListObjectsV2");
  } catch (error) {
    failure = error;
  } finally {
    try {
      runTest(["down", "--volumes", "--remove-orphans"]);
    } catch (cleanupError) {
      console.error(`Test cleanup failed: ${cleanupError.message}`);
      failure ||= cleanupError;
    }
    rmSync(smokeDir, { recursive: true, force: true });
  }
  if (failure) {
    console.error(failure.message);
    process.exitCode = 1;
  }
} else {
  usage();
}
