import { createHash, randomBytes } from "node:crypto";
import {
  lstat,
  mkdir,
  readdir,
  readFile,
  readlink,
  realpath,
  rename,
  writeFile,
} from "node:fs/promises";
import path from "node:path";

const GRAPH_VERSION = 1;
const PROOF_VERSION = 1;
const DIGEST_PATTERN = /^[a-f0-9]{64}$/;

export class GraphValidationError extends Error {
  constructor(message, options) {
    super(message, options);
    this.name = "GraphValidationError";
  }
}

function fail(message, options) {
  throw new GraphValidationError(message, options);
}

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isRfc3339(value) {
  if (typeof value !== "string") {
    return false;
  }
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (match === null) {
    return false;
  }
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, , offsetHourText, offsetMinuteText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const offsetHour = offsetHourText === undefined ? 0 : Number(offsetHourText);
  const offsetMinute = offsetMinuteText === undefined ? 0 : Number(offsetMinuteText);
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59 || offsetHour > 23 || offsetMinute > 59) {
    return false;
  }
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return day >= 1 && day <= daysInMonth[month - 1];
}

function assertOnlyKeys(value, allowed, label) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) {
      fail(`${label} contains unsupported field '${key}'`);
    }
  }
}

function assertRelativePath(value, label) {
  if (typeof value !== "string" || value.length === 0) {
    fail(`${label} must be a non-empty path string`);
  }
  if (path.isAbsolute(value)) {
    fail(`${label} must be relative to the project root`);
  }
}

function resolveInside(root, relativePath, label) {
  assertRelativePath(relativePath, label);
  const absolutePath = path.resolve(root, relativePath);
  const relation = path.relative(root, absolutePath);
  if (relation === ".." || relation.startsWith(`..${path.sep}`) || path.isAbsolute(relation)) {
    fail(`${label} resolves outside the project root: ${relativePath}`);
  }
  return absolutePath;
}

function normalizePath(value) {
  return value.split(path.sep).join("/");
}

async function assertNoSymlinkParents(root, absolutePath, label) {
  const relation = path.relative(root, absolutePath);
  const segments = relation.split(path.sep).filter(Boolean);
  let current = root;

  for (const segment of segments.slice(0, -1)) {
    current = path.join(current, segment);
    try {
      const stats = await lstat(current);
      if (stats.isSymbolicLink()) {
        fail(`${label} traverses symbolic-link directory: ${normalizePath(path.relative(root, current))}`);
      }
    } catch (error) {
      if (error?.code === "ENOENT") {
        return;
      }
      throw error;
    }
  }
}

async function assertPathExists(root, relativePath, label) {
  const absolutePath = resolveInside(root, relativePath, label);
  await assertNoSymlinkParents(root, absolutePath, label);
  try {
    await lstat(absolutePath);
  } catch (error) {
    if (error?.code === "ENOENT") {
      fail(`${label} does not exist: ${relativePath}`);
    }
    throw error;
  }
  return absolutePath;
}

function parseGraph(value) {
  if (!isObject(value)) {
    fail("graph must be a JSON object");
  }
  assertOnlyKeys(value, new Set(["version", "nodes"]), "graph");
  if (value.version !== GRAPH_VERSION) {
    fail(`graph version must be ${GRAPH_VERSION}`);
  }
  if (!Array.isArray(value.nodes) || value.nodes.length === 0) {
    fail("graph must contain at least one node");
  }

  const nodes = value.nodes.map((candidate, index) => {
    const label = `node at index ${index}`;
    if (!isObject(candidate)) {
      fail(`${label} must be an object`);
    }
    assertOnlyKeys(
      candidate,
      new Set(["id", "kind", "inputs", "dependsOn", "proof"]),
      label,
    );
    if (typeof candidate.id !== "string" || candidate.id.length === 0) {
      fail(`${label} must have a non-empty id`);
    }
    if (candidate.kind !== undefined && typeof candidate.kind !== "string") {
      fail(`node '${candidate.id}' kind must be a string`);
    }
    if (!Array.isArray(candidate.inputs)) {
      fail(`node '${candidate.id}' inputs must be an array`);
    }
    for (const [inputIndex, input] of candidate.inputs.entries()) {
      assertRelativePath(input, `node '${candidate.id}' input ${inputIndex}`);
    }
    const dependsOn = candidate.dependsOn ?? [];
    if (!Array.isArray(dependsOn)) {
      fail(`node '${candidate.id}' dependsOn must be an array`);
    }
    for (const dependency of dependsOn) {
      if (typeof dependency !== "string" || dependency.length === 0) {
        fail(`node '${candidate.id}' has an invalid dependency id`);
      }
    }
    if (new Set(dependsOn).size !== dependsOn.length) {
      fail(`node '${candidate.id}' contains duplicate dependencies`);
    }
    if (candidate.proof !== undefined) {
      assertRelativePath(candidate.proof, `node '${candidate.id}' proof`);
    }

    return {
      id: candidate.id,
      kind: candidate.kind ?? "",
      inputs: [...candidate.inputs],
      dependsOn: [...dependsOn],
      ...(candidate.proof === undefined ? {} : { proof: candidate.proof }),
    };
  });

  const byId = new Map();
  for (const node of nodes) {
    if (byId.has(node.id)) {
      fail(`graph contains duplicate node id '${node.id}'`);
    }
    byId.set(node.id, node);
  }
  for (const node of nodes) {
    for (const dependency of node.dependsOn) {
      if (!byId.has(dependency)) {
        fail(`node '${node.id}' depends on missing node '${dependency}'`);
      }
    }
  }

  const visiting = new Set();
  const visited = new Set();
  function visit(node, trail) {
    if (visiting.has(node.id)) {
      fail(`graph contains dependency cycle: ${[...trail, node.id].join(" -> ")}`);
    }
    if (visited.has(node.id)) {
      return;
    }
    visiting.add(node.id);
    for (const dependency of node.dependsOn) {
      visit(byId.get(dependency), [...trail, node.id]);
    }
    visiting.delete(node.id);
    visited.add(node.id);
  }
  for (const node of nodes) {
    visit(node, []);
  }

  return { version: GRAPH_VERSION, nodes, byId };
}

async function readJson(absolutePath, label) {
  let source;
  try {
    source = await readFile(absolutePath, "utf8");
  } catch (error) {
    if (error?.code === "ENOENT") {
      fail(`${label} does not exist: ${absolutePath}`);
    }
    throw error;
  }
  try {
    return JSON.parse(source);
  } catch (error) {
    fail(`${label} is not valid JSON: ${error.message}`, { cause: error });
  }
}

export async function loadGraph(projectRoot = process.cwd(), graphPath = ".groundspec/graph.json") {
  let root;
  try {
    root = await realpath(path.resolve(projectRoot));
  } catch (error) {
    fail(`project root does not exist: ${projectRoot}`, { cause: error });
  }

  const absoluteGraphPath = resolveInside(root, graphPath, "graph path");
  await assertNoSymlinkParents(root, absoluteGraphPath, "graph path");
  const parsed = parseGraph(await readJson(absoluteGraphPath, "graph"));

  for (const node of parsed.nodes) {
    const inputPaths = [];
    for (const [index, input] of node.inputs.entries()) {
      inputPaths.push(await assertPathExists(root, input, `node '${node.id}' input ${index}`));
    }
    if (node.proof !== undefined) {
      const proofPath = resolveInside(root, node.proof, `node '${node.id}' proof`);
      await assertNoSymlinkParents(root, proofPath, `node '${node.id}' proof`);
      for (const inputPath of inputPaths) {
        const relation = path.relative(inputPath, proofPath);
        if (relation === "" || (!relation.startsWith("..") && !path.isAbsolute(relation))) {
          fail(`node '${node.id}' proof must not be contained by its own input '${normalizePath(path.relative(root, inputPath))}'`);
        }
      }
    }
  }

  return {
    root,
    graphPath: normalizePath(path.relative(root, absoluteGraphPath)),
    version: parsed.version,
    nodes: parsed.nodes,
    byId: parsed.byId,
  };
}

function utf8Bytes(value) {
  const bytes = Buffer.from(value, "utf8");
  if (bytes.toString("utf8") !== value) {
    fail("protocol strings must contain valid Unicode scalar values");
  }
  return bytes;
}

function compareUtf8(left, right) {
  return Buffer.compare(utf8Bytes(left), utf8Bytes(right));
}

export function digestFrames(frames) {
  const hash = createHash("sha256");
  for (const [label, value] of frames) {
    const labelBytes = utf8Bytes(label);
    const valueBytes = typeof value === "string" ? utf8Bytes(value) : Buffer.from(value);
    const header = Buffer.allocUnsafe(12);
    header.writeUInt32BE(labelBytes.length, 0);
    header.writeBigUInt64BE(BigInt(valueBytes.length), 4);
    hash.update(header.subarray(0, 4));
    hash.update(labelBytes);
    hash.update(header.subarray(4));
    hash.update(valueBytes);
  }
  return hash.digest("hex");
}

async function digestAbsolutePath(root, absolutePath) {
  const stats = await lstat(absolutePath);
  const relativePath = normalizePath(path.relative(root, absolutePath));

  if (stats.isSymbolicLink()) {
    const target = await readlink(absolutePath, { encoding: "buffer" });
    return digestFrames([
      ["domain", "groundspec/path/v1"],
      ["type", "symlink"],
      ["path", relativePath],
      ["target", target],
    ]);
  }
  if (stats.isFile()) {
    const contents = await readFile(absolutePath);
    return digestFrames([
      ["domain", "groundspec/path/v1"],
      ["type", "file"],
      ["path", relativePath],
      ["content", contents],
    ]);
  }
  if (stats.isDirectory()) {
    const entries = (await readdir(absolutePath, { withFileTypes: true, encoding: "buffer" }))
      .map(({ name }) => {
        const bytes = Buffer.from(name);
        const decoded = bytes.toString("utf8");
        if (!utf8Bytes(decoded).equals(bytes)) {
          fail(`directory contains a non-UTF-8 entry: ${relativePath}`);
        }
        return { bytes, decoded };
      })
      .sort((left, right) => Buffer.compare(left.bytes, right.bytes));
    const frames = [
      ["domain", "groundspec/path/v1"],
      ["type", "directory"],
      ["path", relativePath],
    ];
    for (const entry of entries) {
      const child = path.join(absolutePath, entry.decoded);
      frames.push(["entry-name", entry.bytes]);
      frames.push(["entry-digest", await digestAbsolutePath(root, child)]);
    }
    return digestFrames(frames);
  }
  fail(`unsupported input type: ${relativePath}`);
}

async function digestRelativePath(graph, relativePath, label) {
  const absolutePath = await assertPathExists(graph.root, relativePath, label);
  return digestAbsolutePath(graph.root, absolutePath);
}

export async function computeNodeDigests(graph) {
  const memo = new Map();

  async function compute(node) {
    if (memo.has(node.id)) {
      return memo.get(node.id);
    }
    const frames = [
      ["domain", "groundspec/node/v1"],
      ["graph-version", String(graph.version)],
      ["id", node.id],
      ["kind", node.kind],
    ];
    const inputs = node.inputs
      .map((input) => ({ original: input, normalized: normalizePath(input) }))
      .sort((left, right) => compareUtf8(left.normalized, right.normalized));
    for (const input of inputs) {
      frames.push(["input-path", input.normalized]);
      frames.push([
        "input-digest",
        await digestRelativePath(graph, input.original, `node '${node.id}' input`),
      ]);
    }
    for (const dependency of [...node.dependsOn].sort(compareUtf8)) {
      frames.push(["dependency-id", dependency]);
      frames.push(["dependency-digest", await compute(graph.byId.get(dependency))]);
    }
    const digest = digestFrames(frames);
    memo.set(node.id, digest);
    return digest;
  }

  for (const node of graph.nodes) {
    await compute(node);
  }
  return memo;
}

function parseProof(value, node) {
  if (!isObject(value)) {
    fail(`proof for node '${node.id}' must be an object`);
  }
  assertOnlyKeys(
    value,
    new Set(["version", "node", "digest", "result", "recordedAt", "evidence", "note"]),
    `proof for node '${node.id}'`,
  );
  if (value.version !== PROOF_VERSION) {
    fail(`proof for node '${node.id}' version must be ${PROOF_VERSION}`);
  }
  if (value.node !== node.id) {
    fail(`proof at '${node.proof}' belongs to '${value.node}', not '${node.id}'`);
  }
  if (typeof value.digest !== "string" || !DIGEST_PATTERN.test(value.digest)) {
    fail(`proof for node '${node.id}' has an invalid digest`);
  }
  if (value.result !== "passed" && value.result !== "failed") {
    fail(`proof for node '${node.id}' result must be 'passed' or 'failed'`);
  }
  if (!isRfc3339(value.recordedAt)) {
    fail(`proof for node '${node.id}' has an invalid recordedAt value`);
  }
  if (value.note !== undefined && typeof value.note !== "string") {
    fail(`proof for node '${node.id}' note must be a string`);
  }
  if (value.evidence !== undefined) {
    if (!isObject(value.evidence)) {
      fail(`proof for node '${node.id}' evidence must be an object`);
    }
    assertOnlyKeys(value.evidence, new Set(["path", "digest"]), `proof for node '${node.id}' evidence`);
    assertRelativePath(value.evidence.path, `proof for node '${node.id}' evidence path`);
    if (typeof value.evidence.digest !== "string" || !DIGEST_PATTERN.test(value.evidence.digest)) {
      fail(`proof for node '${node.id}' evidence has an invalid digest`);
    }
  }
  return value;
}

async function readProof(graph, node) {
  const absolutePath = resolveInside(graph.root, node.proof, `node '${node.id}' proof`);
  await assertNoSymlinkParents(graph.root, absolutePath, `node '${node.id}' proof`);
  try {
    return parseProof(JSON.parse(await readFile(absolutePath, "utf8")), node);
  } catch (error) {
    if (error?.code === "ENOENT") {
      return null;
    }
    if (error instanceof SyntaxError) {
      fail(`proof for node '${node.id}' is not valid JSON`, { cause: error });
    }
    throw error;
  }
}

async function evidenceState(graph, node, proof) {
  if (proof.evidence === undefined) {
    return { fresh: true };
  }
  let currentDigest;
  try {
    currentDigest = await digestRelativePath(
      graph,
      proof.evidence.path,
      `proof for node '${node.id}' evidence`,
    );
  } catch (error) {
    if (error instanceof GraphValidationError && /does not exist/.test(error.message)) {
      return { fresh: false, reason: "evidence-missing" };
    }
    throw error;
  }
  if (currentDigest !== proof.evidence.digest) {
    return { fresh: false, reason: "evidence-changed" };
  }
  return { fresh: true };
}

export async function evaluateGraph(graph) {
  const digests = await computeNodeDigests(graph);
  const nodes = [];

  for (const node of graph.nodes) {
    const digest = digests.get(node.id);
    if (node.proof === undefined) {
      nodes.push({ id: node.id, kind: node.kind, status: "declared", digest });
      continue;
    }
    const proof = await readProof(graph, node);
    if (proof === null) {
      nodes.push({ id: node.id, kind: node.kind, status: "missing", digest, proof: node.proof });
      continue;
    }
    if (proof.digest !== digest) {
      nodes.push({
        id: node.id,
        kind: node.kind,
        status: "stale",
        reason: "node-digest-changed",
        digest,
        proof: node.proof,
      });
      continue;
    }
    const evidence = await evidenceState(graph, node, proof);
    if (!evidence.fresh) {
      nodes.push({
        id: node.id,
        kind: node.kind,
        status: "stale",
        reason: evidence.reason,
        digest,
        proof: node.proof,
      });
      continue;
    }
    nodes.push({
      id: node.id,
      kind: node.kind,
      status: proof.result,
      digest,
      proof: node.proof,
      recordedAt: proof.recordedAt,
    });
  }

  const statuses = nodes.map(({ status }) => status);
  const overall = statuses.includes("failed")
    ? "failed"
    : statuses.some((status) => status === "missing" || status === "stale")
      ? "incomplete"
      : "healthy";

  return {
    version: 1,
    graph: graph.graphPath,
    overall,
    nodes,
  };
}

export async function attestNode(graph, nodeId, options) {
  const node = graph.byId.get(nodeId);
  if (node === undefined) {
    fail(`cannot attest missing node '${nodeId}'`);
  }
  if (node.proof === undefined) {
    fail(`node '${nodeId}' does not declare a proof path`);
  }
  if (options?.result !== "passed" && options?.result !== "failed") {
    fail("attestation result must be 'passed' or 'failed'");
  }
  if (options.note !== undefined && typeof options.note !== "string") {
    fail("attestation note must be a string");
  }

  const digests = await computeNodeDigests(graph);
  const proof = {
    version: PROOF_VERSION,
    node: node.id,
    digest: digests.get(node.id),
    result: options.result,
    recordedAt: new Date().toISOString(),
  };
  if (options.evidence !== undefined) {
    proof.evidence = {
      path: normalizePath(options.evidence),
      digest: await digestRelativePath(
        graph,
        options.evidence,
        `attestation evidence for node '${node.id}'`,
      ),
    };
  }
  if (options.note !== undefined) {
    proof.note = options.note;
  }

  const absoluteProofPath = resolveInside(graph.root, node.proof, `node '${node.id}' proof`);
  await assertNoSymlinkParents(graph.root, absoluteProofPath, `node '${node.id}' proof`);
  const proofDirectory = path.dirname(absoluteProofPath);
  await mkdir(proofDirectory, { recursive: true });
  const temporaryPath = path.join(
    proofDirectory,
    `.${path.basename(absoluteProofPath)}.${process.pid}.${randomBytes(6).toString("hex")}.tmp`,
  );
  await writeFile(temporaryPath, `${JSON.stringify(proof, null, 2)}\n`, { flag: "wx" });
  await rename(temporaryPath, absoluteProofPath);
  return proof;
}
