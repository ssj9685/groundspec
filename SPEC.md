# Spec

Complete the existing single-binary GroundSpec workflow with deterministic intake and lifecycle artifacts, provider-neutral CLI adapters, graph-backed verification, safe publication, and portable release behavior while preserving compatibility for workspaces without a declared graph.

## Requirements

### PB-01: Local text-source intake

Accept local HTML, Markdown, and plain-text files in the first intake slice.

Acceptance:

- Each supported local format can be ingested without network access.
- Unsupported formats fail with a clear error.

### PB-02: Deterministic SourceBundle

Emit a deterministic, versioned SourceBundle with source identity, format, raw SHA-256 digest, and ordered text blocks.

Acceptance:

- Identical input produces byte-equivalent canonical output.
- The bundle records all required metadata and ordered blocks.

### PB-03: Stable block metadata

Give every extracted block a stable ID, source line, block kind, section path, and normalized text.

Acceptance:

- All blocks contain every required field.
- Repeated intake preserves IDs and ordering.

### PB-04: Bounded intake semantics

Perform intake without network requests or claims that extracted text is a correct requirement or specification.

Acceptance:

- Intake uses only local input.
- Output distinguishes extraction from semantic approval.

### PB-05: External synthesis boundary

Keep AI and human synthesis outside the deterministic core and exchange synthesis through versioned files.

Acceptance:

- Core workflows accept and emit versioned artifacts.
- No synthesis provider is embedded in the core.

### PB-06: Self-applied graph and proof

Apply the repository graph and proof mechanism to source, specification, design, tests, implementation, and evidence.

Acceptance:

- Declared lifecycle artifacts are represented by current graph evidence.
- Missing required proof prevents completion.

### PB-07: Source-neutral extraction

Do not introduce extraction rules specific to the first external acceptance source.

Acceptance:

- Extraction behavior is selected by declared format rather than source identity.
- Conformance fixtures from different origins use the same format handlers.

### PB-08: Provider-neutral proposal

Represent requirement proposals as provider-neutral artifacts binding every candidate and question to current SourceBundle blocks.

Acceptance:

- Every candidate and question has valid current block references.
- Proposal validation is independent of provider identity.

### PB-09: Structural proposal validation

Validate proposal structure and provenance without deciding semantic correctness.

Acceptance:

- Malformed or stale provenance is rejected.
- Structurally valid proposals are not rejected based on statement meaning.

### PB-10: Separate human review

Record review separately, bind it to exact candidate content, support accept or reject decisions, and resolve proposal questions.

Acceptance:

- Review artifacts do not mutate proposals.
- Every decision and resolution binds to reviewable content.

### PB-11: Review readiness status

Report whether review is blocked or ready for materialization and enumerate every outstanding decision.

Acceptance:

- Status is ready only when no required decision is outstanding.
- Blocked status lists all blockers deterministically.

### PB-12: Versioned adapter protocol

Exchange AI and document-generation work through versioned files; the core neither loads provider SDKs nor executes adapter commands.

Acceptance:

- Adapter inputs and outputs have validated versions.
- Core packages have no provider SDK or adapter-execution dependency.

### PB-13: Workflow self-hosting

Use the proposal, review, and status protocols to develop this workflow slice.

Acceptance:

- Repository evidence includes proposal, review, and status protocol coverage for the slice.
- Self-hosting artifacts pass the same validators as user artifacts.

### PB-14: Safe agent discovery

Discover installed agent CLIs through capability and login-status probes without reading or copying OAuth tokens.

Acceptance:

- Discovery uses argv subprocess probes without a shell.
- No credential files or token values are read or persisted.

### PB-15: Deterministic adapter selection

Honor explicit project adapter selection; auto-select only when exactly one authenticated compatible adapter exists.

Acceptance:

- Explicit compatible selection takes precedence.
- Zero or multiple eligible automatic candidates produce a blocking result.

### PB-16: Workspace bootstrap

From one supported source, create a self-contained workspace, SourceBundle, AI proposal, and review gate.

Acceptance:

- Bootstrap creates all four outputs with linked versions and identities.
- Partial failure does not report the review gate as ready.

### PB-17: Reviewed plan materialization

After review, accept a structured adapter-produced development plan and deterministically materialize SPEC.md, TEST_SPEC.md, DESIGN.md, and an implementation plan.

Acceptance:

- Materialization requires a ready review and valid plan.
- Repeated materialization from identical inputs is deterministic.

### PB-18: Explicit implementation delegation

Only an explicit implementation action delegates workspace changes and records adapter, reviewed plan, changed artifacts, and reported checks.

Acceptance:

- No implementation occurs during planning or review.
- The implementation result records every required provenance field.

### PB-19: Reviewed verification execution

Run only reviewed argv commands without a shell, capture output and exit code, and record fresh graph evidence.

Acceptance:

- Unreviewed or shell-form commands are rejected.
- Each executed command has captured results and newly generated graph evidence.

### PB-20: Complete source support

Accept local HTML, Markdown, text, and text-bearing PDF sources while leaving OCR to an external reader.

Acceptance:

- Text-bearing PDFs are ingested locally.
- Image-only PDFs produce a bounded error directing OCR to an external capability.

### PB-21: Terminal completion gate

Prevent terminal completion until current source, proposal, review, materialization, implementation, verification, and evidence nodes exist.

Acceptance:

- Any missing or stale lifecycle node blocks completion.
- Completion succeeds only when all required nodes are current.

### PB-22: Dogfood failure promotion

Develop each new slice through the latest usable workflow and promote dogfood failures into contracts and regression tests.

Acceptance:

- A promoted failure links a contract to a regression test.
- Workflow evidence identifies the usable workflow version.

### PB-23: Candidate-digest review binding

Bind decisions to individual candidate digests so unchanged decisions survive proposal growth and changed or new candidates block review.

Acceptance:

- Unchanged candidate digests retain decisions.
- New or changed digests are reported as outstanding.

### PB-24: Project-context invariants

Bind planning to reviewed language, framework, source boundary, and constraints and reject silent architecture replacement.

Acceptance:

- Plans reproduce approved project-context invariants exactly.
- Conflicting architecture or source-boundary proposals fail validation.

### PB-25: Optional pinned contract compiler

Allow an executable-contract adapter pinned to mermaid-spec commit 7cde6be8c8e4e6b0e26ac4f5d58873be5aa3163b without adding its compiler or runtime to the Go core or released binary.

Acceptance:

- Pin verification checks the exact commit.
- The released Go binary has no Bun or mermaid-spec runtime dependency.

### PB-26: Explicit contract links

Require stable Mermaid contract identities and explicit implementation, test, consumer, and documentation links; reject missing, duplicate, unknown, or repository-escaping links.

Acceptance:

- Every declared contract has valid explicit link categories.
- Invalid or escaping paths fail verification rather than being inferred.

### PB-27: Bounded impact context

Produce bounded context and propagated impact artifacts containing affected contracts, linked implementation files, and linked tests.

Acceptance:

- Context is derived only from explicit graph links.
- Output is deterministic and bounded for context-free agent use.

### PB-28: Full completion verification

Treat targeted impact tests as advisory and require regenerated artifacts, compiler drift checks, the reviewed full suite, and fresh graph evidence for completion.

Acceptance:

- Passing targeted tests alone cannot complete a lifecycle.
- All full-verification evidence is current at completion.

### PB-29: Safe publication boundary

Publish only explicitly allowlisted repository artifacts with portable adapter provenance, excluding transient context and rejecting local paths, credentials, symlinks, binary content, and oversized files.

Acceptance:

- Publication is an explicit separate action.
- Every prohibited artifact class fails closed before upload.

### PB-30: Portable MIT release

Provide MIT-licensed public reuse through a stable Go install path, versioned self-contained binaries, checksums, and a CI-verified source build.

Acceptance:

- Release targets build self-contained Go binaries and checksums.
- CI verifies the documented source installation path and license.

### PB-31: Safe existing-project initialization

Initialize in a non-empty project without overwriting files and fail closed on reserved workflow-path conflicts.

Acceptance:

- Existing unrelated files remain unchanged.
- Any conflicting reserved path stops initialization before writes.
