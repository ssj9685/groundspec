# GroundSpec

[![CI](https://github.com/ssj9685/groundspec/actions/workflows/ci.yml/badge.svg)](https://github.com/ssj9685/groundspec/actions/workflows/ci.yml)

`groundspec` is a local-first CLI for turning source material into reviewed implementation work with current verification evidence.

The graph core answers one question:

> Does the current state of each declared contract still have current evidence?

The source workflow validates provider-neutral requirement proposals, records human decisions separately, materializes reviewed plans, delegates implementation only on an explicit command, and runs only reviewed argv verification commands.

Deterministic intake, validation, rendering, graph evaluation, and evidence binding stay in the Go core. Semantic synthesis and implementation remain behind an installed agent CLI adapter; no provider SDK or OAuth token access is part of the core.

## Install

With Go 1.27 or newer, copy and paste the complete block below into Bash or Zsh. It installs GroundSpec, adds Go's install directory to the current shell, and verifies the result:

```shell
go install github.com/ssj9685/groundspec/cmd/groundspec@latest

groundspec_bin_dir="$(go env GOBIN)"
if [ -z "$groundspec_bin_dir" ]; then
  groundspec_bin_dir="$(go env GOPATH)/bin"
fi
export PATH="$groundspec_bin_dir:$PATH"

groundspec --version
groundspec --help
```

Successful installation prints `groundspec v0.1.0` or a newer version before the command guide. The `export` applies to the current terminal; add the resolved directory to your shell profile to keep it available in new terminals. PowerShell instructions and command-not-found recovery are in [Getting started](./docs/getting-started.md#1-install).

Alternatively, download a checksummed self-contained archive from [GitHub Releases](https://github.com/ssj9685/groundspec/releases). The released CLI does not require Node.js or Bun. PDF intake additionally uses a locally installed `pdftotext` command.

AI-backed draft, planning, and implementation currently require an authenticated Codex CLI session. GroundSpec discovers the existing login but never reads or copies its OAuth token:

```shell
codex login
groundspec adapters
```

## Use in another project

Initialize GroundSpec inside an existing repository without overwriting current project files:

```shell
cd your-project
groundspec init ./requirements.md --adapter codex-cli
groundspec status .groundspec/proposal.json
```

For a new isolated workspace:

```shell
groundspec start ./requirements.html --output ./workspace --adapter codex-cli
```

Both commands stop at the human review gate. Continue only after reviewing every candidate, then run `materialize`, `implement`, `verify`, and `lifecycle`. See [Getting started](./docs/getting-started.md) for the complete sequence and project-context example.

## Bootstrap flow

This repository develops the checker with the same artifacts it checks:

```text
PRODUCT_BRIEF.md
→ SourceBundle
→ proposal.json
→ review.json
→ requirements.json
→ SPEC.md
→ TEST_SPEC.md
→ language-neutral fixtures
→ failing implementation test
→ DESIGN.md
→ JS reference + Go implementation
→ conformance proof
→ self-check
```

## Commands

Run both independent implementations' tests:

```shell
npm test
go test ./...
```

Build the end-user CLI as a self-contained executable:

```shell
go build -trimpath -o groundspec ./cmd/groundspec
./groundspec check
```

Ingest a local HTML, Markdown, text, or text-bearing PDF source without assigning semantic meaning:

```shell
./groundspec ingest PRODUCT_BRIEF.md --json > .groundspec/sources/product-brief.json
```

The resulting `SourceBundle` preserves source identity, visible text blocks, section paths, and line locations. A human or external AI creates a proposal; explicit review decisions remain separate, and the deterministic core does not silently turn extracted prose into accepted requirements.

PDF intake uses a local `pdftotext` executable. A PDF with no extractable text returns an explicit request for an external OCR reader.

Discover authenticated agent CLIs and bootstrap a self-contained workspace:

```shell
./groundspec adapters
./groundspec start ./brief.md --output ./workspace --adapter codex-cli
```

After every candidate is reviewed, materialize and explicitly execute the remaining stages:

```shell
./groundspec materialize .groundspec/proposal.json --adapter codex-cli
./groundspec implement .groundspec/development-plan.json --adapter codex-cli
./groundspec verify .groundspec/development-plan.json
./groundspec lifecycle
```

`materialize` deterministically writes the four reviewed documents from structured adapter output. `implement` records the selected adapter, actual artifact inventory and changes, and reported checks. `verify` executes the plan's argv vectors directly, captures combined output and exit codes, and binds each log into the versioned verification record.

Verification commands are completion-gating by default. A reviewed command may set `optional: true`; it still runs and records its failure, but that failure does not make the aggregate verification fail. Generated archives and verification-output directories are derived evidence and are excluded from the implementation source snapshot.

Validate a proposal produced by any external human or AI adapter:

```shell
./groundspec proposal validate .groundspec/proposal.json --json
```

Check the human-decision gate. A missing review is a normal blocked state and exits with `1`:

```shell
./groundspec status .groundspec/proposal.json
```

Record one decision at a time without modifying the proposal:

```shell
./groundspec review .groundspec/proposal.json --accept PB-01 --note "Confirmed from the source"
./groundspec review .groundspec/proposal.json --reject PB-02 --note "Out of scope"
./groundspec review .groundspec/proposal.json --resolve Q-01 --answer "Use a 500 meter radius"
```

The default review path is `.groundspec/review.json`; use `--review <path>` for another workflow. `status` becomes `ready` only after every requirement is accepted or rejected and every question is resolved. Readiness means the review gate is complete, not that the proposed meaning is correct.

Node.js and Go are contributor requirements only. A released executable does not require either runtime on the end user's machine.

Tagged releases build the CGO-disabled targets declared in [`release/targets.json`](./release/targets.json), publish one archive per platform, and attach SHA-256 checksums.

The JavaScript implementation is a temporary conformance oracle, not a second release product. It is removed after the v0 protocol corpus and supported-platform binary checks are complete; the shipped core remains one Go executable.

## Executable contract harness

The optional Mermaid harness is pinned to commit `7cde6be8c8e4e6b0e26ac4f5d58873be5aa3163b` as a Bun-only development dependency. It compiles the reviewed lifecycle diagrams in `contracts/`, validates the explicit repository-local links in `mermaid-spec.links.json`, and keeps the generated contract graph in `contracts/generated/`.

```shell
bun install
bun run contracts:build
bun run contracts:verify
bun run contracts:context
bun run contracts:impact:test
```

`contracts:context` writes the bounded bundle for `machine:GroundSpecLifecycle` to the ignored local file `.groundspec/evidence/mermaid-context.json` and prints only a small verification summary. Impact tests prove that a changed lifecycle contract selects its linked Go implementation and tests. Targeted impact checks remain advisory. Repositories that declare `.groundspec/graph.json` (or pass `--graph`) also require a fresh passing `terminal-lifecycle` node; graph-free workspaces retain the core reviewed-plan and verification lifecycle.

## Publication boundary

The product never uploads, commits, or pushes files. Before a separate explicit publication step, verify the exact Git candidate set against [`publication-policy.json`](./publication-policy.json):

```shell
npm run publication:verify
```

The audit is fail-closed: a new path must be explicitly allowlisted, transient agent context is ignored, persisted adapter provenance omits local executable paths and authentication details, and local user paths, credential-shaped text, symlinks, binary content, or oversized files block publication. The audit reports only the file and violated rule, never the matched value.

Create separate evidence and attest each verification node only after its command succeeds:

```shell
npm run test:report
go test -json ./... > .groundspec/evidence/go-tests.json
go run ./cmd/groundspec attest PRODUCT-BRIEF --result passed --evidence .groundspec/sources/product-brief.json
go run ./cmd/groundspec attest REQUIREMENT-PROPOSAL --result passed --evidence .groundspec/evidence/proposal-validation.json
go run ./cmd/groundspec attest REQUIREMENT-REVIEW --result passed --evidence .groundspec/evidence/workflow-status.json
go run ./cmd/groundspec attest CORE-REQUIREMENTS --result passed
go run ./cmd/groundspec attest JS-VERIFY --result passed --evidence .groundspec/evidence/js-tests.tap
go run ./cmd/groundspec attest GO-VERIFY --result passed --evidence .groundspec/evidence/go-tests.json
go run ./cmd/groundspec attest RELEASE-VERIFY --result passed --evidence .groundspec/evidence/groundspec-release-matrix.json
go run ./cmd/groundspec attest MERMAID-CONTRACT --result passed --evidence test-or-verification/commands/V-05.log
go run ./cmd/groundspec attest PUBLICATION-VERIFY --result passed --evidence test-or-verification/commands/V-07.log
go run ./cmd/groundspec attest LIFECYCLE-VERIFY --result passed --evidence .groundspec/verification.json
```

Check the repository:

```shell
go run ./cmd/groundspec check
go run ./cmd/groundspec check --json
```

The command exits with `0` only when the graph is valid and every proof-bearing node has fresh, passing evidence. It exits with `1` for missing, stale, or failed evidence and `2` for invalid input or usage.

## Documents

- [Behavior contract](./SPEC.md)
- [Design](./DESIGN.md)
- [Verification contract](./TEST_SPEC.md)
- [Bootstrap source](./PRODUCT_BRIEF.md)
- [Graph protocol](./schemas/contract-graph.schema.json)
- [Proof protocol](./schemas/proof.schema.json)
- [Project context protocol](./schemas/project-context.schema.json)
- [Development plan protocol](./schemas/development-plan.schema.json)
- [Implementation result protocol](./schemas/implementation-result.schema.json)
- [Verification result protocol](./schemas/verification-result.schema.json)
- [Lifecycle status protocol](./schemas/lifecycle-status.schema.json)
- [Source bundle protocol](./schemas/source-bundle.schema.json)
- [Requirement ledger protocol](./schemas/requirement-ledger.schema.json)
- [Requirement proposal protocol](./schemas/requirement-proposal.schema.json)
- [Requirement review protocol](./schemas/requirement-review.schema.json)
- [Workflow status protocol](./schemas/workflow-status.schema.json)
- [Proposal adapter boundary](./integrations/proposal-adapter/README.md)
- [Optional mermaid-spec integration](./integrations/mermaid-spec/README.md)

## Current boundary

GroundSpec is an MIT-licensed pre-release CLI. It supports local HTML, Markdown, plain text, and text-bearing PDF intake and one authenticated Codex CLI adapter. Provider SDKs, built-in OCR, automatic review approval, and automatic Git publication remain outside the product boundary.
