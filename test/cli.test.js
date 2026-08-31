import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const cli = path.resolve("bin/groundspec.js");

async function makeProject(graph) {
  const root = await mkdtemp(path.join(os.tmpdir(), "groundspec-cli-"));
  await writeFile(path.join(root, "graph.json"), `${JSON.stringify(graph, null, 2)}\n`);
  return root;
}

function run(root, ...args) {
  return spawnSync(process.execPath, [cli, ...args], {
    cwd: root,
    encoding: "utf8",
  });
}

test("TV-08: check exposes stable JSON and exit codes", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "T-01", inputs: ["test.js"], proof: ".proofs/T-01.json" }],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(path.join(root, "test.js"), "test v1\n");

  let result = run(root, "check", "--graph", "graph.json", "--json");
  assert.equal(result.status, 1, result.stderr);
  assert.equal(JSON.parse(result.stdout).overall, "incomplete");

  result = run(root, "attest", "T-01", "--graph", "graph.json", "--result", "passed", "--evidence", "");
  assert.equal(result.status, 2, result.stderr);

  result = run(root, "attest", "T-01", "--graph", "graph.json", "--result", "passed");
  assert.equal(result.status, 0, result.stderr);

  result = run(root, "check", "--graph", "graph.json", "--json");
  assert.equal(result.status, 0, result.stderr);
  assert.equal(JSON.parse(result.stdout).overall, "healthy");
});

test("TV-08: invalid graphs exit with code 2", async (t) => {
  const root = await makeProject({ version: 1, nodes: [] });
  t.after(() => rm(root, { recursive: true, force: true }));

  const result = run(root, "check", "--graph", "graph.json", "--json");
  assert.equal(result.status, 2);
  assert.match(result.stderr, /at least one node/i);
});

test("TV-10: JavaScript CLI matches the shared conformance exit code", async () => {
  const fixtureRoot = path.resolve("testdata", "conformance", "v1");
  const expected = JSON.parse(await readFile(path.join(fixtureRoot, "expected.json"), "utf8"));
  const result = run(fixtureRoot, "check", "--graph", "graph.json", "--json");

  assert.equal(result.status, expected.exitCode, result.stderr);
  assert.equal(JSON.parse(result.stdout).overall, expected.overall);
});
