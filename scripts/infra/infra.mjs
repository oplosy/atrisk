import { spawn, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
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

async function runInfraTest() {
  const smokeDir = mkdtempSync(resolve(tmpdir(), "atrisk-infra-smoke-"));
  // Keep the project name bounded for Docker resource-name limits while making
  // concurrent runs independent on the same host.
  const runToken = randomBytes(8).toString("hex");
  const testProject = `atrisk-test-${runToken}`;
  if (testProject.length > 63) {
    throw new Error(`Generated Compose project name is too long: ${testProject}`);
  }
  const testEnvironment = {
    ...process.env,
    COMPOSE_PROJECT_NAME: testProject,
    POSTGRES_DB: "atrisk_test",
    POSTGRES_USER: "atrisk_test",
    POSTGRES_PASSWORD: "atlasrisk-test-only",
    POSTGRES_PORT: "",
    GARAGE_ACCESS_KEY: "GKatrisktest",
    GARAGE_SECRET_KEY: "atlasrisk-garage-test-only-secret",
    GARAGE_BUCKET: "atrisk-test-raw",
    GARAGE_S3_PORT: "",
    SMOKE_OUTPUT_DIR: smokeDir,
  };
  const testBase = ["compose", "-f", composeFile, "--project-name", testProject];
  let activeCommand;
  const runTest = (args, capture = false) => {
    let child;
    try {
      child = spawn("docker", [...testBase, "--profile", "test", ...args], {
        cwd: repoRoot,
        env: testEnvironment,
        stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit",
      });
    } catch (error) {
      return Promise.reject(error);
    }
    let stdout = "";
    let stderr = "";
    if (capture) {
      child.stdout.setEncoding("utf8");
      child.stderr.setEncoding("utf8");
      child.stdout.on("data", (chunk) => {
        stdout += chunk;
      });
      child.stderr.on("data", (chunk) => {
        stderr += chunk;
      });
    }
    const operation = new Promise((resolveOperation, rejectOperation) => {
      let settled = false;
      const settle = (callback, value) => {
        if (settled) return;
        settled = true;
        callback(value);
      };
      child.once("error", (error) => settle(rejectOperation, error));
      child.once("close", (status) => {
        if (status !== 0) {
          if (capture && stderr) process.stderr.write(stderr);
          settle(rejectOperation, new Error(`docker compose test command exited with status ${status}`));
          return;
        }
        settle(resolveOperation, capture ? stdout : "");
      });
    });
    const trackedOperation = operation.finally(() => {
      if (activeCommand?.promise === trackedOperation) activeCommand = undefined;
    });
    activeCommand = { child, promise: trackedOperation };
    return trackedOperation;
  };
  const smoke = (args) => runTest(["run", "--rm", "--no-deps", "s3-smoke", ...args], true);
  const endpoint = ["--endpoint-url", "http://garage:3900"];
  const bucket = testEnvironment.GARAGE_BUCKET;
  const key = "smoke/put-get-head-list.txt";
  const payload = readFileSync(resolve(repoRoot, "infra/compose/smoke-payload.txt"));
  const outputFile = resolve(smokeDir, "received.txt");
  let failure;
  let signalExitCode;
  let testStarted = false;
  let cleanupPromise;
  const ensureNotInterrupted = () => {
    if (signalExitCode) throw new Error(`Infrastructure test interrupted by signal (exit ${signalExitCode})`);
  };
  const cleanupTest = async () => {
    if (cleanupPromise) return cleanupPromise;
    cleanupPromise = (async () => {
      const active = activeCommand;
      if (active) {
        active.child.kill();
        try {
          await active.promise;
        } catch {
          // The interrupted command's failure is handled by the main test flow.
        }
      }
      if (testStarted) {
        try {
          await runTest(["down", "--volumes", "--remove-orphans"]);
        } catch (cleanupError) {
          console.error(`Test cleanup failed: ${cleanupError.message}`);
          failure ||= cleanupError;
        }
      }
      rmSync(smokeDir, { recursive: true, force: true });
    })();
    return cleanupPromise;
  };
  const handleSignal = (signal) => {
    if (signalExitCode) return;
    signalExitCode = signal === "SIGINT" ? 130 : 143;
    console.warn(`Received ${signal}; cleaning up test project ${testProject}.`);
    activeCommand?.child.kill();
    void cleanupTest();
  };
  process.once("SIGINT", handleSignal);
  process.once("SIGTERM", handleSignal);
  process.once("exit", () => rmSync(smokeDir, { recursive: true, force: true }));
  try {
    testStarted = true;
    await runTest(["up", "-d", "--wait", "postgres", "garage"]);
    ensureNotInterrupted();
    await runTest(["ps"]);
    ensureNotInterrupted();
    await smoke(["s3api", "put-object", "--bucket", bucket, "--key", key, "--body", "/aws/smoke-payload.txt", ...endpoint]);
    ensureNotInterrupted();
    await smoke(["s3api", "get-object", "--bucket", bucket, "--key", key, "/aws/smoke-output/received.txt", ...endpoint]);
    ensureNotInterrupted();
    const received = readFileSync(outputFile);
    if (!received.equals(payload)) throw new Error("S3 Get body did not match the Put body");
    const head = JSON.parse(await smoke(["s3api", "head-object", "--bucket", bucket, "--key", key, ...endpoint]));
    ensureNotInterrupted();
    if (head.ContentLength !== payload.length) throw new Error("S3 Head ContentLength did not match the Put body");
    const listed = (await smoke(["s3api", "list-objects-v2", "--bucket", bucket, "--prefix", "smoke/", "--query", "Contents[?Key==`smoke/put-get-head-list.txt`].Key", "--output", "text", ...endpoint])).trim();
    ensureNotInterrupted();
    if (listed !== key) throw new Error(`S3 List did not return ${key}; received ${JSON.stringify(listed)}`);
    console.log("S3 smoke passed: PutObject, GetObject, HeadObject, ListObjectsV2");
  } catch (error) {
    failure = error;
  } finally {
    await cleanupTest();
    process.removeListener("SIGINT", handleSignal);
    process.removeListener("SIGTERM", handleSignal);
  }
  if (signalExitCode) {
    process.exitCode = signalExitCode;
  } else if (failure) {
    console.error(failure.message);
    process.exitCode = 1;
  }
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
  await runInfraTest();
} else {
  usage();
}
