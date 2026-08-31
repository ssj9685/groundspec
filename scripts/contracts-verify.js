import {
  compileProject,
  loadTraceConfiguration,
  testProject,
  validateTraceConfiguration,
  verifyProject,
} from "mermaid-spec";

await import("./verify-contract-pin.js");

const contracts = new URL("../contracts", import.meta.url).pathname;
const generated = new URL("../contracts/generated", import.meta.url).pathname;
const links = new URL("../mermaid-spec.links.json", import.meta.url).pathname;

const project = await compileProject(contracts);
const examples = testProject(project);
if (examples.length === 0 || examples.some((example) => !example.passed)) {
  throw new Error("one or more reviewed Mermaid contract examples failed");
}

const drift = await verifyProject(contracts, generated);
if (!drift.valid) {
  throw new Error(`generated Mermaid artifacts have drift: ${drift.drift.join(", ")}`);
}

const trace = await loadTraceConfiguration(links);
const validation = await validateTraceConfiguration(drift.project.graph, trace);
if (!validation.valid) {
  throw new Error(validation.diagnostics.join("\n"));
}

console.log(`Verified ${examples.length} contract examples, ${Object.keys(drift.project.artifacts).length} generated artifacts, and ${validation.links.length} explicit trace links`);
