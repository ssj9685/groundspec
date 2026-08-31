import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  GraphValidationError,
  attestNode,
  digestFrames,
  evaluateGraph,
  loadGraph,
} from "../src/core.js";

async function makeProject(graph) {
  const root = await mkdtemp(path.join(os.tmpdir(), "groundspec-"));
  await writeFile(path.join(root, "graph.json"), `${JSON.stringify(graph, null, 2)}\n`);
  return root;
}

async function writeProjectFile(root, relativePath, contents) {
  const target = path.join(root, relativePath);
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, contents);
}

test("TV-09: frame digests are language-neutral and byte-exact", () => {
  const digest = digestFrames([
    ["domain", Buffer.from("groundspec/test/v1")],
    ["empty", Buffer.alloc(0)],
    ["unicode", Buffer.from("한🙂")],
  ]);

  assert.equal(
    digest,
    "cb635b95f0a9a18ac1b469cf34c92ae6e9cc8271afbc2863f29563b9b516cf4d",
  );
});

test("TV-10: JavaScript matches the shared conformance fixture", async () => {
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const fixtureRoot = path.join(repositoryRoot, "testdata", "conformance", "v1");
  const expected = JSON.parse(await readFile(path.join(fixtureRoot, "expected.json"), "utf8"));
  const report = await evaluateGraph(await loadGraph(fixtureRoot, "graph.json"));

  assert.equal(report.overall, expected.overall);
  assert.deepEqual(
    report.nodes.map(({ id, status, digest }) => ({ id, status, digest })),
    expected.nodes,
  );
});

test("TV-09: non-UTF-8 repository entry names are rejected", {
  skip: process.platform === "win32",
}, async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "A", inputs: ["suite"] }],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, "suite"));
  const invalidPath = Buffer.concat([
    Buffer.from(`${path.join(root, "suite")}${path.sep}`),
    Buffer.from([0xff]),
  ]);
  try {
    await writeFile(invalidPath, "invalid name\n");
  } catch {
    t.skip("filesystem rejects non-UTF-8 names before the checker");
    return;
  }

  const graph = await loadGraph(root, "graph.json");
  await assert.rejects(evaluateGraph(graph), GraphValidationError);
});

test("TV-01 and TV-02: a missing proof becomes fresh after attestation", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [
      { id: "SC-01", kind: "behavior", inputs: ["SPEC.md"] },
      {
        id: "T-01",
        kind: "verification",
        inputs: ["TEST_SPEC.md", "test.js"],
        dependsOn: ["SC-01"],
        proof: ".proofs/T-01.json",
      },
    ],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeProjectFile(root, "SPEC.md", "requirement v1\n");
  await writeProjectFile(root, "TEST_SPEC.md", "verification v1\n");
  await writeProjectFile(root, "test.js", "test v1\n");

  const graph = await loadGraph(root, "graph.json");
  const before = await evaluateGraph(graph);
  assert.equal(before.overall, "incomplete");
  assert.equal(before.nodes.find(({ id }) => id === "T-01").status, "missing");

  await attestNode(graph, "T-01", { result: "passed" });
  const after = await evaluateGraph(graph);
  assert.equal(after.overall, "healthy");
  assert.equal(after.nodes.find(({ id }) => id === "T-01").status, "passed");
});

test("TV-03: changing an upstream input makes dependent proof stale", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [
      { id: "SC-01", inputs: ["SPEC.md"] },
      {
        id: "T-01",
        inputs: ["test.js"],
        dependsOn: ["SC-01"],
        proof: ".proofs/T-01.json",
      },
    ],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeProjectFile(root, "SPEC.md", "requirement v1\n");
  await writeProjectFile(root, "test.js", "test v1\n");

  const graph = await loadGraph(root, "graph.json");
  await attestNode(graph, "T-01", { result: "passed" });
  await writeProjectFile(root, "SPEC.md", "requirement v2\n");

  const report = await evaluateGraph(graph);
  assert.equal(report.overall, "incomplete");
  assert.equal(report.nodes.find(({ id }) => id === "T-01").status, "stale");
});

test("TV-04: a fresh failed attestation keeps the graph failed", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "T-01", inputs: ["test.js"], proof: ".proofs/T-01.json" }],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeProjectFile(root, "test.js", "test v1\n");

  const graph = await loadGraph(root, "graph.json");
  await attestNode(graph, "T-01", { result: "failed" });
  const report = await evaluateGraph(graph);
  assert.equal(report.overall, "failed");
  assert.equal(report.nodes[0].status, "failed");
});

test("TV-05: missing dependencies and cycles are rejected", async (t) => {
  const missingRoot = await makeProject({
    version: 1,
    nodes: [{ id: "A", inputs: [], dependsOn: ["UNKNOWN"] }],
  });
  const cycleRoot = await makeProject({
    version: 1,
    nodes: [
      { id: "A", inputs: [], dependsOn: ["B"] },
      { id: "B", inputs: [], dependsOn: ["A"] },
    ],
  });
  t.after(() => rm(missingRoot, { recursive: true, force: true }));
  t.after(() => rm(cycleRoot, { recursive: true, force: true }));

  await assert.rejects(loadGraph(missingRoot, "graph.json"), GraphValidationError);
  await assert.rejects(loadGraph(cycleRoot, "graph.json"), GraphValidationError);
});

test("TV-06: paths outside the project root are rejected", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "A", inputs: ["../outside.txt"] }],
  });
  t.after(() => rm(root, { recursive: true, force: true }));

  await assert.rejects(loadGraph(root, "graph.json"), GraphValidationError);
});

test("TV-06: attestation evidence cannot escape the project root", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "T-01", inputs: ["test.js"], proof: ".proofs/T-01.json" }],
  });
  const outsidePath = path.join(path.dirname(root), `${path.basename(root)}-outside.txt`);
  t.after(() => rm(root, { recursive: true, force: true }));
  t.after(() => rm(outsidePath, { force: true }));
  await writeProjectFile(root, "test.js", "test v1\n");
  await writeFile(outsidePath, "outside\n");

  const graph = await loadGraph(root, "graph.json");
  await assert.rejects(
    attestNode(graph, "T-01", {
      result: "passed",
      evidence: `../${path.basename(outsidePath)}`,
    }),
    GraphValidationError,
  );
});

test("TV-06: a proof cannot be stored inside its own input directory", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "T-01", inputs: ["state"], proof: "state/T-01.json" }],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeProjectFile(root, "state/input.txt", "input\n");

  await assert.rejects(loadGraph(root, "graph.json"), GraphValidationError);
});

test("TV-07: changed or missing evidence makes a proof stale", async (t) => {
  const root = await makeProject({
    version: 1,
    nodes: [{ id: "T-01", inputs: ["test.js"], proof: ".proofs/T-01.json" }],
  });
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeProjectFile(root, "test.js", "test v1\n");
  await writeProjectFile(root, "results/test.xml", "passed v1\n");

  const graph = await loadGraph(root, "graph.json");
  await attestNode(graph, "T-01", {
    result: "passed",
    evidence: "results/test.xml",
  });
  await writeProjectFile(root, "results/test.xml", "changed\n");
  let report = await evaluateGraph(graph);
  assert.equal(report.nodes[0].status, "stale");

  const proof = JSON.parse(await readFile(path.join(root, ".proofs/T-01.json"), "utf8"));
  assert.equal(proof.evidence.path, "results/test.xml");
  await rm(path.join(root, "results/test.xml"));
  report = await evaluateGraph(graph);
  assert.equal(report.nodes[0].status, "stale");
});
