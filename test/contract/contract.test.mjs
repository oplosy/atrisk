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

const jobSchema = await readJson("contracts/schemas/job-envelope.schema.json");
const supportedVersion = jobSchema.properties.schema_version.const;
const requiredJobKeys = jobSchema.required;
const allowedJobKeys = Object.keys(jobSchema.properties);

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
  const unknown = Object.keys(value).filter((key) => !allowedJobKeys.includes(key));
  if (jobSchema.additionalProperties === false && unknown.length > 0) {
    return { code: "VALIDATION_ERROR", message: `Unknown fields: ${unknown.join(", ")}` };
  }
  const snapshotIds = value.input_snapshot_ids;
  const snapshotSchema = jobSchema.properties.input_snapshot_ids;
  if (
    Array.isArray(snapshotIds) &&
    snapshotSchema.uniqueItems === true &&
    new Set(snapshotIds).size !== snapshotIds.length
  ) {
    return { code: "VALIDATION_ERROR", message: "input_snapshot_ids must be unique" };
  }
  if (
    typeof value.kind !== jobSchema.properties.kind.type ||
    !new RegExp(jobSchema.properties.kind.pattern).test(value.kind) ||
    value.kind.length < jobSchema.properties.kind.minLength ||
    typeof value.idempotency_key !== jobSchema.properties.idempotency_key.type ||
    value.idempotency_key.length < jobSchema.properties.idempotency_key.minLength ||
    value.idempotency_key.length > jobSchema.properties.idempotency_key.maxLength ||
    !Array.isArray(value.input_snapshot_ids) ||
    value.input_snapshot_ids.some(
      (id) =>
        typeof id !== snapshotSchema.items.type ||
        id.length < snapshotSchema.items.minLength,
    ) ||
    !value.payload ||
    typeof value.payload !== jobSchema.properties.payload.type ||
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

test("quality evaluation contract carries fixed cutoffs and gate states", async () => {
  const openapi = await readJson("contracts/openapi/openapi.json");
  const operation = openapi.paths["/api/v1/quality/evaluate"].post;
  assert.equal(operation.operationId, "evaluateDataQuality");
  assert.equal(
    operation.requestBody.content["application/json"].schema.$ref,
    "#/components/schemas/QualityEvaluationRequest",
  );
  assert.deepEqual(
    openapi.components.schemas.QualityInputResult.properties.classification.enum,
    ["fresh", "stale", "missing", "partial", "suspect", "revised"],
  );
  assert.deepEqual(openapi.components.schemas.QualityInputResult.properties.state.enum, [
    "valid",
    "degraded",
    "blocked",
  ]);
  assert.ok(openapi.components.schemas.QualityEvaluationRequest.required.includes("as_of"));
  assert.match(openapi.components.schemas.QualityEvaluationRequest.properties.to.description, /exclusive/i);
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

test("duplicate snapshot IDs fail the schema-derived validator", async () => {
  const result = validateJob(
    await readJson("test/contract/fixtures/invalid/job-duplicate-snapshot.json"),
  );
  assert.equal(result.code, "VALIDATION_ERROR");
  assert.match(result.message, /unique/);
});

test("additional top-level properties fail the schema-derived validator", async () => {
  const result = validateJob(
    await readJson("test/contract/fixtures/invalid/job-extra-property.json"),
  );
  assert.equal(result.code, "VALIDATION_ERROR");
  assert.match(result.message, /Unknown fields/);
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

test("generated Go models preserve schema enum and literal distinctions", async () => {
  const go = await readFile(resolve(root, "contracts/generated/go/contracts.go"), "utf8");
  assert.match(go, /type ResultEnvelopeStatus string/);
  assert.match(go, /Status\s+ResultEnvelopeStatus/);
  assert.match(go, /ResultEnvelopeStatusSucceeded ResultEnvelopeStatus = "succeeded"/);
  assert.match(go, /type JobEnvelopeSchemaVersion string/);
  assert.match(go, /JobEnvelopeSchemaVersionV1_0 JobEnvelopeSchemaVersion = "1\.0"/);
});

test("portfolio resource schemas are represented in every generated contract target", async () => {
  const openapi = await readJson("contracts/openapi/openapi.json");
  for (const name of ["Portfolio", "Account", "Snapshot"]) {
    assert.ok(openapi.components.schemas[name], name);
  }
  const [go, typescript, python] = await Promise.all([
    readFile(resolve(root, "contracts/generated/go/contracts.go"), "utf8"),
    readFile(resolve(root, "contracts/generated/typescript/contracts.ts"), "utf8"),
    readFile(resolve(root, "contracts/generated/python/contracts.py"), "utf8"),
  ]);
  for (const name of ["Portfolio", "Account", "Snapshot"]) {
    assert.match(go, new RegExp(`type ${name} struct`), name);
    assert.match(typescript, new RegExp(`export interface ${name}`), name);
    assert.match(python, new RegExp(`class ${name}\\(ContractModel\\)`), name);
  }
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
  assert.equal(jobSchema.additionalProperties, false);
  assert.equal(jobSchema.properties.input_snapshot_ids.uniqueItems, true);
  assert.equal(jobSchema.properties.input_snapshot_ids.items.minLength, 1);
  const resultSchema = await readJson("contracts/schemas/result-envelope.schema.json");
  assert.equal(resultSchema.additionalProperties, false);
  assert.equal(resultSchema.properties.input_snapshot_ids.uniqueItems, true);
  assert.equal(resultSchema.properties.input_snapshot_ids.items.minLength, 1);
});
