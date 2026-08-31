import packageDocument from "../package.json" with { type: "json" };
import { readdir } from "node:fs/promises";
import { join, relative } from "node:path";

const revision = "7cde6be8c8e4e6b0e26ac4f5d58873be5aa3163b";
const expected = `github:ssj9685/mermaid-spec#${revision}`;
const expectedTreeDigest = "3daabe01cdd8e3fbbcc35741aae933770da56b138d53825b5e8c2bcdf9fdbee8";

async function packageFiles(root, directory = root) {
  const result = [];
  for (const entry of (await readdir(directory, { withFileTypes: true })).sort((left, right) => left.name.localeCompare(right.name))) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) result.push(...await packageFiles(root, path));
    else if (entry.isFile()) result.push(relative(root, path).replaceAll("\\", "/"));
  }
  return result;
}

async function treeDigest(root) {
  const entries = [];
  for (const path of await packageFiles(root)) {
    const content = await Bun.file(join(root, path)).arrayBuffer();
    const sha256 = new Bun.CryptoHasher("sha256").update(content).digest("hex");
    entries.push({ path, sha256 });
  }
  return new Bun.CryptoHasher("sha256").update(JSON.stringify(entries)).digest("hex");
}

if (packageDocument.devDependencies?.["mermaid-spec"] !== expected) {
  throw new Error(`mermaid-spec must remain pinned to ${revision}`);
}

const installedRoot = new URL("../node_modules/mermaid-spec", import.meta.url).pathname;
const installed = await Bun.file(join(installedRoot, "package.json")).json();
if (installed.name !== "mermaid-spec" || installed.version !== "0.3.0") {
  throw new Error("installed mermaid-spec package is incompatible with the reviewed pin");
}
const installedTreeDigest = await treeDigest(installedRoot);
if (installedTreeDigest !== expectedTreeDigest) {
  throw new Error(`installed mermaid-spec tree does not match reviewed revision: ${installedTreeDigest}`);
}

console.log(`Verified mermaid-spec pin ${revision}`);
