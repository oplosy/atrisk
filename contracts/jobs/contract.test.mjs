import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const readJson = async (path) => JSON.parse(await readFile(path, "utf8"));

test("risk job and result schemas are versioned contract sources", async () => {
  for (const name of ["risk-job", "risk-result"]) {
    const schema = await readJson(`contracts/jobs/${name}.schema.json`);
    assert.equal(schema.$schema, "https://json-schema.org/draft/2020-12/schema");
    assert.equal(schema.properties.schema_version.const, "1.0");
    assert.equal(schema.additionalProperties, false);
  }
});

test("scenario revaluation jobs bind one immutable version to a snapshot", async () => {
  const schema = await readJson("contracts/jobs/scenario-revalue.schema.json");
  assert.equal(schema.$schema, "https://json-schema.org/draft/2020-12/schema");
  assert.deepEqual(schema.required, ["scenario_id", "scenario_version", "snapshot_id", "positions", "pre_metrics"]);
  assert.equal(schema.properties.scenario_version.properties.version.minimum, 1);
  assert.equal(schema.additionalProperties, false);
});

test("golden job and result fixtures match their envelope contracts", async () => {
  const job = await readJson("test/fixtures/risk/golden-job.json");
  const result = await readJson("test/fixtures/risk/golden-result.json");
  assert.equal(job.schema_version, "1.0");
  assert.equal(new Set(job.input_snapshot_ids).size, job.input_snapshot_ids.length);
  assert.equal(result.schema_version, "1.0");
  assert.equal(result.status, "succeeded");
  assert.equal(result.data_quality, "healthy");
});
