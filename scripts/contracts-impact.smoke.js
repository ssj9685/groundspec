import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  compareContractGraphs,
  compileProject,
  loadTraceConfiguration,
  validateTraceConfiguration,
} from "mermaid-spec";

async function verifyLifecycleImpact(temporaryDirectories) {
  const repository = new URL("..", import.meta.url).pathname;
  const contracts = join(repository, "contracts");
  const baseline = await Bun.file(join(contracts, "generated", "contract-graph.generated.json")).json();
  const temporary = await mkdtemp(join(tmpdir(), "groundspec-contracts-"));
  temporaryDirectories.push(temporary);

  for (const file of ["workflow-lifecycle.md", "dogfood-failure-lifecycle.md"]) {
    let source = await Bun.file(join(contracts, file)).text();
    if (file === "workflow-lifecycle.md") {
      source = source.replace("recordFullVerification", "recordReviewedFullVerification");
    }
    await Bun.write(join(temporary, file), source);
  }

  const current = await compileProject(temporary);
  const trace = await loadTraceConfiguration(join(repository, "mermaid-spec.links.json"));
  const validation = await validateTraceConfiguration(current.graph, trace, {
    additionalContractIds: baseline.nodes.map((node) => node.id),
  });
  assert.deepEqual(validation.diagnostics, []);

  const impact = compareContractGraphs(current.graph, baseline, validation.links);
  assert.ok(impact.changes.modified.includes("transition:GroundSpecLifecycle:TargetedVerified:verify"));
  assert.ok(impact.affected.includes("machine:GroundSpecLifecycle"));
  assert.ok(impact.tests.includes("internal/pipeline/pipeline_test.go"));
  assert.ok(impact.reviewRequired.includes("internal/pipeline/pipeline.go"));
}

async function verifyTargetedVerificationBoundary() {
  const project = await compileProject(new URL("../contracts", import.meta.url).pathname);
  const machine = project.machines.find((candidate) => candidate.name === "GroundSpecLifecycle");
  assert.ok(machine);
  assert.equal(machine.transitions.some((transition) => transition.from === "TargetedVerified" && transition.to === "Complete"), false);
  assert.equal(machine.transitions.some((transition) => transition.from === "TargetedVerified" && transition.to === "FullyVerified"), true);
}

async function verifyInvalidLinksFailClosed(temporaryDirectories) {
  const repository = new URL("..", import.meta.url).pathname;
  const project = await compileProject(join(repository, "contracts"));
  const temporary = await mkdtemp(join(tmpdir(), "groundspec-invalid-links-"));
  temporaryDirectories.push(temporary);
  const linksPath = join(temporary, "mermaid-spec.links.json");
  await Bun.write(linksPath, `${JSON.stringify({
    version: 1,
    links: [
      { contract: "machine:Missing", role: "implementation", path: "missing.go" },
      { contract: "machine:GroundSpecLifecycle", role: "test", path: "../internal/pipeline/pipeline_test.go" },
    ],
    requirements: [{ kind: "machine", roles: ["implementation", "test"] }],
  })}\n`);
  const trace = await loadTraceConfiguration(linksPath);
  const validation = await validateTraceConfiguration(project.graph, trace);
  assert.equal(validation.valid, false);
  assert.ok(validation.diagnostics.some((item) => item.includes("unknown contract 'machine:Missing'")));
  assert.ok(validation.diagnostics.some((item) => item.includes("must stay inside the trace directory")));
  assert.ok(validation.diagnostics.some((item) => item.includes("requires a test link")));
}

const temporaryDirectories = [];
try {
  await verifyLifecycleImpact(temporaryDirectories);
  await verifyTargetedVerificationBoundary();
  await verifyInvalidLinksFailClosed(temporaryDirectories);
  console.log("Verified contract impact, completion boundary, and fail-closed trace links");
} finally {
  await Promise.all(temporaryDirectories.map((directory) => rm(directory, { recursive: true, force: true })));
}
