import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { promisify } from "node:util";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const run = promisify(execFile);
const readJson = async (relativePath) =>
  JSON.parse(await readFile(resolve(root, relativePath), "utf8"));

const supportedVersion = "1.0";
const requiredJobKeys = [
  "kind",
  "schema_version",
  "idempotency_key",
  "input_snapshot_ids",
  "payload",
];

function validateJob(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return { code: "VALIDATION_ERROR", message: "Job must be an object" };
  }
  if (value.schema_version !== supportedVersion) {
    return {
      code: "ATLAS_UNKNOWN_SCHEMA_VERSION",
      message: "Unsupported schema version",
      request_id: "contract-test-request",
      details: {
        kind: typeof value.kind === "string" ? value.kind : "unknown",
        schema_version: value.schema_version,
        supported_versions: [supportedVersion],
      },
    };
  }
  const missing = requiredJobKeys.filter((key) => !(key in value));
  if (missing.length > 0) {
    return { code: "VALIDATION_ERROR", message: `Missing fields: ${missing.join(", ")}` };
  }
  if (
    typeof value.kind !== "string" ||
    !/^[a-z][a-z0-9_.-]*$/.test(value.kind) ||
    typeof value.idempotency_key !== "string" ||
    value.idempotency_key.length === 0 ||
    !Array.isArray(value.input_snapshot_ids) ||
    value.input_snapshot_ids.some((id) => typeof id !== "string" || id.length === 0) ||
    !value.payload ||
    typeof value.payload !== "object" ||
    Array.isArray(value.payload)
  ) {
    return { code: "VALIDATION_ERROR", message: "Job fields are invalid" };
  }
  return null;
}

test("OpenAPI source is 3.1 and carries request/idempotency conventions", async () => {
  const openapi = await readJson("contracts/openapi/openapi.json");
  assert.equal(openapi.openapi, "3.1.0");
  assert.ok(openapi.components.parameters.RequestId);
  assert.ok(openapi.components.parameters.IdempotencyKey);
  assert.equal(openapi.paths.constructor, Object);
});

test("valid and additive job fixtures pass validation", async () => {
  for (const file of ["job.json", "job-additive-change.json"]) {
    assert.equal(validateJob(await readJson(`test/contract/fixtures/valid/${file}`)), null, file);
  }
});

test("invalid job fixture fails with a validation error", async () => {
  const result = validateJob(
    await readJson("test/contract/fixtures/invalid/job-missing-idempotency-key.json"),
  );
  assert.equal(result.code, "VALIDATION_ERROR");
  assert.match(result.message, /idempotency_key/);
});

test("unknown schema versions fail with a stable machine-readable error", async () => {
  const result = validateJob(
    await readJson("test/contract/fixtures/invalid/job-unknown-version.json"),
  );
  assert.deepEqual(result, {
    code: "ATLAS_UNKNOWN_SCHEMA_VERSION",
    message: "Unsupported schema version",
    request_id: "contract-test-request",
    details: {
      kind: "risk.run",
      schema_version: "9.0",
      supported_versions: ["1.0"],
    },
  });
});

test("generated Python models compile and import when Pydantic is available", async (t) => {
  const model = resolve(root, "contracts/generated/python/contracts.py");
  const packageFile = resolve(root, "contracts/generated/python/__init__.py");
  await run("python", ["-m", "py_compile", model, packageFile]);
  try {
    await run("python", ["-c", "import pydantic"]);
  } catch {
    t.skip("Pydantic is not installed in this Python environment");
    return;
  }
  const env = { ...process.env, PYTHONPATH: resolve(root, "contracts/generated/python") };
  await run(
    "python",
    [
      "-c",
      "from pydantic import ValidationError; from contracts import JobEnvelope, UnknownSchemaVersionDetails; JobEnvelope(kind='risk.run', schema_version='1.0', idempotency_key='k', input_snapshot_ids=[], payload={});\nfor ids in (['same', 'same'], ['']):\n    try: JobEnvelope(kind='risk.run', schema_version='1.0', idempotency_key='k', input_snapshot_ids=ids, payload={}); raise SystemExit('invalid snapshot ids accepted')\n    except ValidationError: pass\ntry: UnknownSchemaVersionDetails(kind='risk.run', schema_version='9.0'); raise SystemExit('missing supported versions accepted')\nexcept ValidationError: pass\ntry: UnknownSchemaVersionDetails(kind='risk.run', schema_version='9.0', supported_versions=['1.0', '1.0']); raise SystemExit('non-canonical supported versions accepted')\nexcept ValidationError: pass",
    ],
    { env },
  );
});

test("all JSON Schema sources declare a draft and stable version const", async () => {
  const names = [
    "error-envelope",
    "job-envelope",
    "result-envelope",
    "import-manifest",
    "version-error",
  ];
  for (const name of names) {
    const schema = await readJson(`contracts/schemas/${name}.schema.json`);
    assert.equal(schema.$schema, "https://json-schema.org/draft/2020-12/schema", name);
    if (["job-envelope", "result-envelope", "import-manifest"].includes(name)) {
      assert.equal(schema.properties.schema_version.const, "1.0", name);
    }
  }
});
