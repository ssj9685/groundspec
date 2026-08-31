# mermaid-spec integration

`mermaid-spec` can be used as an optional deterministic contract compiler without becoming part of the `groundspec` core.

The tested integration boundary is:

```text
reviewed Mermaid Markdown
→ mermaid-spec build/test/verify
→ generated contracts + command evidence
→ groundspec attest
→ freshness check
```

## Install a pinned revision

The project is not currently published in the npm registry. Pin the reviewed Git commit rather than following a moving branch:

```shell
bun add --dev 'github:ssj9685/mermaid-spec#7cde6be8c8e4e6b0e26ac4f5d58873be5aa3163b'
```

The reviewed revision identifies itself as version 0.3.0 and is MIT licensed. The repository pin check also verifies the installed package tree digest, so a same-version replacement does not silently satisfy the contract. Re-review and update both values deliberately when adopting a later revision.

## Compile and verify

```shell
bun run contracts:build
bun run contracts:verify
bun run contracts:context
bun run contracts:impact:test
```

The package manifest, Bun lockfile, pin verifier, `contracts/`, generated contract graph, and `mermaid-spec.links.json` should be declared as inputs of the executable-contract node. This binds the compiler revision, reviewed diagrams, generated output, and explicit code/test links together. Record successful output as external evidence only after drift, links, bounded context, impact, and the full reviewed suite succeed.

`contracts:context` keeps the complete source-bearing bundle in the ignored local evidence directory and writes only its subject, counts, and byte size to command logs. The bounded agent input therefore stays available locally without becoming a publication candidate.

An example node is provided in [`graph-node.example.json`](./graph-node.example.json). Merge it into the project's own graph and replace node IDs or paths as needed.

## Why this remains external

- The Go release keeps no Node.js or Mermaid dependency.
- `groundspec` does not execute repository-defined commands.
- Teams may replace `mermaid-spec` with another compiler without migrating the core.
- Generated drift becomes stale through ordinary graph inputs and evidence digests.

Targeted impact results select work; they are not terminal evidence. Regenerate and verify contracts, run the complete reviewed verification suite, and refresh repository graph evidence before reporting completion.
