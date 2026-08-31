import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { auditBytes, auditPortableAdapterRecord, auditPublication, isAllowed, validatePublicationPolicy } from "../src/publication.js";

const policy = {
  version: 1,
  maxFileBytes: 128,
  allowedFiles: ["README.md"],
  allowedPrefixes: ["src/"],
};

test("PB-29: publication candidates require an explicit allowlist entry", () => {
  assert.equal(isAllowed("README.md", policy), true);
  assert.equal(isAllowed("src/core.js", policy), true);
  assert.equal(isAllowed("private/session.json", policy), false);
  assert.equal(isAllowed("private/session.json", { ...policy, allowedPrefixes: [""] }), false);
  assert.equal(isAllowed("../README.md", policy), false);
});

test("PB-29: malformed publication policy fails closed", () => {
  assert.deepEqual(validatePublicationPolicy({ version: 1, maxFileBytes: 0, allowedFiles: ["../README.md"], allowedPrefixes: [""] }), [
    "publication policy maxFileBytes must be a positive integer",
    "publication policy allowedFiles must contain safe relative files",
    "publication policy allowedPrefixes must contain safe relative directory prefixes",
  ]);
});

test("PB-29: publication audit rejects binary content", () => {
  assert.deepEqual(auditBytes(Buffer.from([0])), ["binary content is not allowlisted"]);
});

test("PB-29: publication audit rejects invalid UTF-8 text", () => {
  assert.deepEqual(auditBytes(Buffer.from([0xff])), ["text is not valid UTF-8"]);
});

test("PB-29: publication audit rejects oversized allowlisted files before upload", (context) => {
  const root = mkdtempSync(join(tmpdir(), "groundspec-publication-"));
  context.after(() => rmSync(root, { recursive: true, force: true }));
  execFileSync("git", ["init", "--quiet"], { cwd: root });
  writeFileSync(join(root, "README.md"), "x".repeat(129));

  const report = auditPublication(root, policy);
  assert.equal(report.passed, false);
  assert.deepEqual(report.failures, [{ path: "README.md", violations: ["exceeds 128 bytes"] }]);
});

test("PB-29: persisted adapter records allow only portable provenance", () => {
  const path = ".groundspec/implementation-result.json";
  const portable = Buffer.from(JSON.stringify({ adapter: { executable: "codex", auth: "authenticated", ready: true } }));
  assert.deepEqual(auditPortableAdapterRecord(path, portable), []);

  const local = Buffer.from(JSON.stringify({
    adapter: {
      executable: "/opt/example/bin/codex",
      auth: "Logged in using ChatGPT",
      ready: true,
      reason: "local probe detail",
    },
  }));
  assert.deepEqual(auditPortableAdapterRecord(path, local), [
    "adapter provenance contains authentication detail",
    "adapter provenance contains a local executable path",
    "adapter provenance contains local probe detail",
  ]);
});

test("PB-29: publication audit rejects symlinks without following them", (context) => {
  const root = mkdtempSync(join(tmpdir(), "groundspec-publication-"));
  context.after(() => rmSync(root, { recursive: true, force: true }));
  execFileSync("git", ["init", "--quiet"], { cwd: root });
  const outside = join(root, "..", `${basename(root)}-outside.txt`);
  writeFileSync(outside, "outside\n");
  context.after(() => rmSync(outside, { force: true }));
  symlinkSync(outside, join(root, "linked.txt"));

  const report = auditPublication(root, { version: 1, maxFileBytes: 128, allowedFiles: ["linked.txt"], allowedPrefixes: [] });
  assert.equal(report.passed, false);
  assert.deepEqual(report.failures, [{ path: "linked.txt", violations: ["symbolic links are not allowed in the public artifact set"] }]);
});
