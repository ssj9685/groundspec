import {
  compileProject,
  createContractContext,
  loadTraceConfiguration,
  validateTraceConfiguration,
} from "mermaid-spec";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const project = await compileProject(new URL("../contracts", import.meta.url).pathname);
const trace = await loadTraceConfiguration(new URL("../mermaid-spec.links.json", import.meta.url).pathname);
const validation = await validateTraceConfiguration(project.graph, trace);
if (!validation.valid) throw new Error(validation.diagnostics.join("\n"));

const context = await createContractContext(
  project.graph,
  "machine:GroundSpecLifecycle",
  validation.links,
  { includeFiles: true, maxBytes: 65_536 },
);
const output = `${JSON.stringify(context, null, 2)}\n`;
const maximumBundleBytes = 131_072;
if (Buffer.byteLength(output) > maximumBundleBytes) {
  throw new Error(`contract context exceeds ${maximumBundleBytes} bytes`);
}
const evidencePath = fileURLToPath(new URL("../.groundspec/evidence/mermaid-context.json", import.meta.url));
await mkdir(dirname(evidencePath), { recursive: true });
await writeFile(evidencePath, output, { mode: 0o600 });
process.stdout.write(`${JSON.stringify({
  version: 1,
  subject: context.subject.id,
  dependencies: context.dependencies.length,
  links: context.links.length,
  linkedFiles: context.linkedFiles.length,
  bundleBytes: Buffer.byteLength(output),
  localArtifact: ".groundspec/evidence/mermaid-context.json",
})}\n`);
