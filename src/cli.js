import { attestNode, evaluateGraph, GraphValidationError, loadGraph } from "./core.js";

export class CliUsageError extends Error {
  constructor(message) {
    super(message);
    this.name = "CliUsageError";
  }
}

function usage(message) {
  throw new CliUsageError(message);
}

function parseOptions(args, allowed) {
  const options = {};
  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index];
    if (!argument.startsWith("--")) {
      usage(`unexpected argument '${argument}'`);
    }
    const name = argument.slice(2);
    const definition = allowed[name];
    if (definition === undefined) {
      usage(`unknown option '--${name}'`);
    }
    if (definition === "boolean") {
      options[name] = true;
      continue;
    }
    const value = args[index + 1];
    if (value === undefined || value.startsWith("--")) {
      usage(`option '--${name}' requires a value`);
    }
    options[name] = value;
    index += 1;
  }
  return options;
}

function humanReport(report) {
  const lines = [`groundspec: ${report.overall}`];
  const symbols = {
    declared: "·",
    passed: "✓",
    failed: "✗",
    missing: "?",
    stale: "!",
  };
  for (const node of report.nodes) {
    const reason = node.reason === undefined ? "" : ` (${node.reason})`;
    lines.push(`${symbols[node.status]} ${node.id}: ${node.status}${reason}`);
  }
  return `${lines.join("\n")}\n`;
}

export async function runCli(args, io = {}) {
  const stdout = io.stdout ?? process.stdout;
  const cwd = io.cwd ?? process.cwd();
  const command = args[0];

  if (command === "check") {
    const options = parseOptions(args.slice(1), { graph: "value", json: "boolean" });
    const graph = await loadGraph(cwd, options.graph ?? ".groundspec/graph.json");
    const report = await evaluateGraph(graph);
    stdout.write(options.json ? `${JSON.stringify(report, null, 2)}\n` : humanReport(report));
    return report.overall === "healthy" ? 0 : 1;
  }

  if (command === "attest") {
    const nodeId = args[1];
    if (nodeId === undefined || nodeId.startsWith("--")) {
      usage("attest requires a node id");
    }
    const options = parseOptions(args.slice(2), {
      graph: "value",
      result: "value",
      evidence: "value",
      note: "value",
      json: "boolean",
    });
    if (options.result === undefined) {
      usage("attest requires '--result passed|failed'");
    }
    const graph = await loadGraph(cwd, options.graph ?? ".groundspec/graph.json");
    const proof = await attestNode(graph, nodeId, {
      result: options.result,
      evidence: options.evidence,
      note: options.note,
    });
    stdout.write(
      options.json
        ? `${JSON.stringify(proof, null, 2)}\n`
        : `attested ${proof.node}: ${proof.result}\n`,
    );
    return 0;
  }

  usage("usage: groundspec check [--graph path] [--json] | groundspec attest <node> --result passed|failed [--evidence path] [--note text] [--graph path] [--json]");
}

export function isExpectedCliError(error) {
  return error instanceof GraphValidationError || error instanceof CliUsageError;
}
