import { execFileSync } from "node:child_process";
import { lstatSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { TextDecoder } from "node:util";

const textRules = [
  ["private key material", /-----BEGIN\s+[A-Z ]*PRIVATE\s+KEY-----/],
  ["OpenAI-style credential", /sk-[A-Za-z0-9_-]{16,}/],
  ["GitHub-style credential", /gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}/],
  ["AWS access identifier", /AKIA[0-9A-Z]{16}/],
  ["bearer credential", /Bearer\s+[A-Za-z0-9._~+/-]{16,}/i],
  ["assigned credential", /[A-Z0-9_]*(?:SECRET|TOKEN|API_KEY|PASSWORD)\s*[:=]\s*["']?[^\s"']{8,}/],
  ["macOS user-home path", /\/(?:Users)\/[^/\s"']+\//],
  ["Linux user-home path", /\/(?:home)\/[^/\s"']+\//],
  ["Windows user-home path", /[A-Za-z]:\\(?:Users)\\[^\\\s"']+\\/i],
];

function isSafeRelativePath(path, prefix = false) {
  if (typeof path !== "string" || path.length === 0 || path.startsWith("/") || path.includes("\\")) return false;
  if (prefix !== path.endsWith("/")) return false;
  const value = prefix ? path.slice(0, -1) : path;
  return value.length > 0 && value.split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

export function validatePublicationPolicy(policy) {
  const violations = [];
  if (policy?.version !== 1) violations.push("publication policy version must be 1");
  if (!Number.isSafeInteger(policy?.maxFileBytes) || policy.maxFileBytes <= 0) violations.push("publication policy maxFileBytes must be a positive integer");
  if (!Array.isArray(policy?.allowedFiles) || policy.allowedFiles.some((path) => !isSafeRelativePath(path))) violations.push("publication policy allowedFiles must contain safe relative files");
  if (!Array.isArray(policy?.allowedPrefixes) || policy.allowedPrefixes.some((path) => !isSafeRelativePath(path, true))) violations.push("publication policy allowedPrefixes must contain safe relative directory prefixes");
  return violations;
}

export function isAllowed(path, policy) {
  if (!isSafeRelativePath(path)) return false;
  return policy.allowedFiles.some((allowed) => isSafeRelativePath(allowed) && path === allowed)
    || policy.allowedPrefixes.some((prefix) => isSafeRelativePath(prefix, true) && path.startsWith(prefix));
}

export function publicationCandidates(root) {
  const output = execFileSync("git", ["-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z"]);
  return output.toString("utf8").split("\0").filter(Boolean).sort();
}

export function auditBytes(bytes) {
  const violations = [];
  if (bytes.includes(0)) return ["binary content is not allowlisted"];
  let text;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return ["text is not valid UTF-8"];
  }
  for (const [rule, expression] of textRules) {
    if (expression.test(text)) violations.push(rule);
  }
  return violations;
}

export function auditPortableAdapterRecord(path, bytes) {
  if (path !== ".groundspec/development-plan.json" && path !== ".groundspec/implementation-result.json") return [];
  let document;
  try {
    document = JSON.parse(bytes.toString("utf8"));
  } catch {
    return ["adapter protocol record is not valid JSON"];
  }
  const adapter = document?.adapter;
  if (!adapter || typeof adapter !== "object" || Array.isArray(adapter)) return ["adapter protocol record has no adapter identity"];
  const violations = [];
  if (adapter.ready !== true || adapter.auth !== "authenticated") violations.push("adapter provenance contains authentication detail");
  if (typeof adapter.executable === "string" && /[/\\]/.test(adapter.executable)) violations.push("adapter provenance contains a local executable path");
  if (typeof adapter.reason === "string" && adapter.reason.length > 0) violations.push("adapter provenance contains local probe detail");
  return violations;
}

export function auditPublication(root, policy) {
  const policyViolations = validatePublicationPolicy(policy);
  if (policyViolations.length > 0) {
    return { version: 1, checked: [], failures: [{ path: "publication-policy.json", violations: policyViolations }], passed: false };
  }
  const checked = publicationCandidates(root);
  const failures = [];
  for (const path of checked) {
    const violations = [];
    if (!isAllowed(path, policy)) violations.push("not in the explicit publication allowlist");
    const absolute = join(root, path);
    const stats = lstatSync(absolute);
    if (stats.isSymbolicLink()) {
      violations.push("symbolic links are not allowed in the public artifact set");
      failures.push({ path, violations });
      continue;
    }
    if (stats.size > policy.maxFileBytes) {
      violations.push(`exceeds ${policy.maxFileBytes} bytes`);
      failures.push({ path, violations });
      continue;
    }
    const bytes = readFileSync(absolute);
    violations.push(...auditBytes(bytes));
    violations.push(...auditPortableAdapterRecord(path, bytes));
    if (violations.length > 0) failures.push({ path, violations });
  }
  return { version: 1, checked, failures, passed: failures.length === 0 };
}
