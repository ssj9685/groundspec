# Product Brief

This is the manually approved bootstrap seed for `groundspec`.

## Product objective

Given source material in different formats, help a human and an AI move from source evidence to specifications, design, tests, implementation, and fresh verification evidence without hiding assumptions or unresolved decisions.

## Bootstrap requirements

- `PB-01`: The first intake slice accepts local HTML, Markdown, and plain-text files.
- `PB-02`: Intake emits a deterministic, versioned `SourceBundle` containing source identity, format, raw SHA-256 digest, and ordered text blocks.
- `PB-03`: Every block has a stable ID, source line, block kind, section path, and normalized text.
- `PB-04`: Intake performs no network request and makes no claim that extracted text is a correct requirement or specification.
- `PB-05`: AI or human synthesis remains outside the deterministic core and communicates through versioned files.
- `PB-06`: The repository uses its own graph and proof mechanism on its source, specification, design, tests, implementation, and evidence.
- `PB-07`: The first external acceptance source may not introduce source-specific extraction rules into the product.
- `PB-08`: A requirement proposal is a provider-neutral artifact that binds every candidate and question to current `SourceBundle` blocks.
- `PB-09`: The core validates proposal provenance and structure without deciding whether a proposed statement is semantically correct.
- `PB-10`: Human review is recorded separately from the proposal, binds to the exact reviewable candidate content, and can accept or reject requirements and resolve questions.
- `PB-11`: The core reports whether review is blocked or ready for specification materialization and identifies every outstanding decision.
- `PB-12`: External AI and document-generation adapters communicate through versioned files; the core does not load a provider SDK or execute adapter commands.
- `PB-13`: The repository uses the proposal, review, and status protocols to develop this workflow slice itself.
- `PB-14`: The product discovers installed agent CLIs through safe capability and login-status probes and never reads or copies their OAuth tokens.
- `PB-15`: An explicit project adapter selection wins; automatic selection is allowed only when exactly one authenticated compatible adapter is available.
- `PB-16`: Starting from one supported source creates a self-contained workspace, deterministic SourceBundle, AI requirement proposal, and review gate.
- `PB-17`: After review, the selected adapter produces a structured development plan from which the core deterministically materializes `SPEC.md`, `TEST_SPEC.md`, `DESIGN.md`, and an implementation plan.
- `PB-18`: An explicit implementation action delegates workspace changes to the authenticated agent and records the adapter, reviewed plan, changed artifacts, and reported checks.
- `PB-19`: An explicit verification action runs only reviewed argv-style commands without a shell, captures their outputs and exit codes, and records fresh graph evidence.
- `PB-20`: The first complete product accepts local HTML, Markdown, text, and text-bearing PDF sources; OCR remains an external reader capability.
- `PB-21`: A project cannot report terminal completion until source, proposal, review, materialization, implementation, verification, and evidence nodes all exist and are current.
- `PB-22`: The repository develops every new slice through its own latest usable workflow and promotes dogfood failures into product contracts and regression tests.
- `PB-23`: Review decisions bind to individual candidate digests so unchanged decisions survive proposal growth while new or changed candidates become explicit blockers.
- `PB-24`: Development planning binds to reviewed project context such as an existing language, framework, source boundary, and constraints, and the core rejects silent architecture replacement.
- `PB-25`: An optional executable-contract adapter may pin an exact external compiler revision without making that compiler or its runtime a dependency of the Go core or released binary.
- `PB-26`: Mermaid contracts use stable contract identities and explicit implementation, test, consumer, and documentation links; missing, duplicate, unknown, or repository-escaping links fail verification instead of being inferred.
- `PB-27`: A contract change can produce bounded context and propagated impact artifacts for a context-free agent, including affected contracts, linked implementation files, and linked tests.
- `PB-28`: Targeted impact tests are advisory; terminal completion still requires regenerated artifacts, compiler drift verification, the reviewed full verification suite, and fresh graph evidence.
- `PB-29`: Publication is a separate explicit boundary that exposes only allowlisted repository artifacts, stores portable adapter provenance, excludes transient context, and rejects local user paths, credential-shaped material, symlinks, binary content, and oversized files before upload.
- `PB-30`: GroundSpec is publicly reusable under the MIT license through a stable Go install path, versioned self-contained binaries, checksums, and a CI-verified source build.
- `PB-31`: A user can initialize GroundSpec in an existing non-empty project without overwriting existing files, while reserved workflow-path conflicts fail closed.

## Decision boundary

The deterministic core preserves source evidence. An external human or AI may classify blocks as explicit requirements, inferred requirements, assumptions, or unresolved decisions. A separate review may reject candidates. The core must not silently convert inference into fact.

An AI proposal is never an approval. Every proposed requirement and unresolved question must receive a separate human review decision before the workflow reports that specification materialization is ready.

## v0 non-goals

- image, audio, or built-in OCR ingestion;
- extracting, copying, or exposing another tool's authentication token;
- silently choosing one provider when multiple authenticated adapters are available;
- automatic acceptance of generated specifications;
- silently replacing an established implementation language or source boundary during planning;
- executing AI-proposed verification commands without an explicit implementation or verification action;
- browser submission or other external publication;
- guaranteeing that a hiring assignment or product review will pass.
- claiming that an AI change is semantically correct solely because a contract-to-file link exists.
- uploading, committing, or pushing any artifact automatically.

## Terminal acceptance scenario

From a new workspace containing only one supported source and an available authenticated agent CLI, the product can produce a reviewed requirement set, materialized specification set, runnable implementation, executed verification evidence, and a healthy complete lifecycle report without an API key or direct access to the agent's OAuth token.
