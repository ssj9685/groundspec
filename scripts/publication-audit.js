#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { auditPublication } from "../src/publication.js";

const root = resolve(process.argv[2] ?? ".");
const policy = JSON.parse(readFileSync(resolve(root, "publication-policy.json"), "utf8"));
const report = auditPublication(root, policy);

if (!report.passed) {
  for (const failure of report.failures) {
    process.stderr.write(`${failure.path}: ${failure.violations.join(", ")}\n`);
  }
  process.exitCode = 1;
} else {
  process.stdout.write(`Publication boundary verified for ${report.checked.length} explicitly allowed files.\n`);
}
