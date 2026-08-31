# Test Spec

## Verification scope

The v0 suite verifies graph integrity, deterministic dependency hashing, proof state transitions, evidence freshness, path containment, and the public CLI boundary. It does not attempt to measure whether a test is semantically sufficient.

## Test cases

| ID | SPEC | Level | Given | When | Then |
|---|---|---|---|---|---|
| TV-01 | SD-01, SD-03 | core | valid graph without a proof | evaluate | proof-bearing node is `missing` and overall state is `incomplete` |
| TV-02 | SD-02, SD-03, SD-05 | core | a missing proof | attest current node as passed | the node becomes `passed` and overall state becomes `healthy` |
| TV-03 | SD-02, SD-03 | core | a current passing proof | change an upstream input | the dependent proof becomes `stale` |
| TV-04 | SD-03, SD-05 | core | a current failed attestation | evaluate | the node is `failed` and overall state is `failed` |
| TV-05 | SD-01 | contract | graph with a missing dependency or cycle | load | graph validation fails with an actionable error |
| TV-06 | SD-01, SD-05 | security | input or evidence path escapes the root | load or attest | the operation is rejected |
| TV-07 | SD-03, SD-05 | core | a passing proof with external evidence | change or remove the evidence file | the proof becomes `stale` |
| TV-08 | SD-04 | CLI | healthy, incomplete, and invalid projects | run `check` | output and exit codes match the public contract |
| TV-09 | SD-02 | protocol | fixed frame and filesystem vectors | hash with each implementation | every implementation produces the recorded SHA-256 digest |
| TV-10 | SD-01–SD-05 | conformance | the same graph, files, proof, and evidence | run JavaScript and Go implementations | node digest, status, overall state, and exit code are identical |
| TV-11 | SD-06 | release | a target declared in `release/targets.json` | build and inspect a release artifact | it executes and has no Node, Go, or other language runtime dependency |
| TV-12 | SD-07 | intake | the same HTML, Markdown, or text source | ingest repeatedly | byte-equivalent source bundles are produced |
| TV-13 | SD-07 | intake | HTML containing metadata, styles, scripts, entities, headings, and list items | ingest | only visible blocks are emitted with decoded text and source lines |
| TV-14 | SD-07 | intake | Markdown containing headings, paragraphs, lists, and fenced code | ingest | ordered typed blocks preserve the section path and source lines |
| TV-15 | SD-07 | CLI | a source outside the project root or an unsupported format | ingest | the operation is rejected with exit code `2` |
| TV-16 | SD-08 | workflow | a current SourceBundle and a proposal whose IDs reference its blocks | validate the proposal repeatedly | the same valid report is produced |
| TV-17 | SD-08 | workflow | a stale bundle, modified bundle, duplicate ID, or missing block reference | validate the proposal | the operation is rejected with an actionable error |
| TV-18 | SD-09 | workflow | a valid proposal and an empty review | accept, reject, or resolve one valid ID | a separate review bound to the proposal is atomically written |
| TV-19 | SD-09 | workflow | an existing review bound to older proposal bytes or an ID of the wrong kind | record a review action | the operation is rejected without changing the review |
| TV-20 | SD-10 | workflow | a valid proposal with missing human decisions | report status | every missing decision is reported and exit code is `1` |
| TV-21 | SD-10 | workflow | every requirement is accepted or rejected and every question is resolved | report status repeatedly | the same `ready` report is produced and exit code is `0` |
| TV-22 | SD-11 | architecture | an external producer using only the published proposal protocol | replace the producer identity and validate its output | core behavior remains provider-neutral and no adapter command is executed |
| TV-23 | SD-09, SD-10 | workflow | a reviewed proposal gains a new candidate or changes one existing candidate | report status | unchanged decisions remain current, the new candidate is undecided, and only the changed candidate is stale |
| TV-24 | PB-14, PB-15 | adapter | installed agent CLIs are missing, unauthenticated, unique, or ambiguous | discover and resolve | probes never read tokens, explicit selection wins, and auto-selection succeeds only for one ready adapter |
| TV-25 | PB-16 | lifecycle | a supported source and deterministic fixture adapter | start a new workspace | source, SourceBundle, proposal, and an explicit blocked review gate are created |
| TV-26 | PB-17, PB-24 | planning | a ready review and approved project context | materialize a plan | every accepted requirement is mapped and architecture replacement is rejected |
| TV-27 | PB-18 | implementation | an explicit implementation action | delegate and inventory | actual source snapshot and changes are recorded while private, generated, evidence, and archive trees are excluded |
| TV-28 | PB-19 | verification | reviewed argv and shell-form candidates | validate and execute | direct argv runs without a shell and shell-capable forms are rejected |
| TV-29 | PB-19, PB-21 | verification | an optional command fails after all required commands pass | record verification | failure evidence remains visible while aggregate completion stays passed |
| TV-30 | PB-20 | intake | a text-bearing or image-only PDF and an external reader | ingest | text becomes blocks and empty extraction requests external OCR explicitly |
| TV-31 | PB-21, PB-22 | lifecycle | a complete project and then a changed source artifact | report lifecycle | complete becomes blocked at the exact stale stage and dogfood regression coverage preserves the behavior |
| TV-32 | PB-23 | workflow | a previous proposal whose candidates still reference unchanged source blocks | redraft after adding source material | the adapter must reproduce every current candidate byte-for-byte or its output is rejected and corrected |
| TV-33 | PB-25, PB-26 | executable contract | the exact reviewed mermaid-spec revision and repository-local trace links | install, build, and verify | the package tree, generated artifacts, stable IDs, required roles, and path containment all validate |
| TV-34 | PB-27 | agent context | a temporary Mermaid lifecycle change and a committed baseline graph | calculate impact and request bounded context | the affected lifecycle selects its linked Go implementation and tests and the context bundle remains size-bounded |
| TV-35 | PB-28 | lifecycle | targeted impact evidence without the remaining reviewed commands | validate terminal verification | targeted success is retained as evidence but cannot replace the full verification record |
| TV-36 | PB-29 | security | explicit publication policy plus safe, unlisted, oversized, binary, symlinked, local-path-bearing, and credential-shaped candidates | run publication verification and persist adapter provenance | only allowlisted safe text files pass and stored adapter metadata contains no machine-local path or authentication mechanism |
| TV-37 | PB-30 | release | a clean checkout and tagged release configuration | run CI, `go install`, and archive builds | source verification passes and each declared archive contains one self-contained GroundSpec binary with a recorded checksum |
| TV-38 | PB-31 | integration | an existing non-empty project and a supported source | initialize once and then repeat with reserved paths present | existing files remain byte-identical, the first run creates `.groundspec`, and conflicting runs fail before writing |

## Bootstrap evidence

The repository declares `CORE-TEST-SPEC` separately from executable verification. It separates `REQUIREMENT-PROPOSAL`, `REQUIREMENT-REVIEW`, `JS-VERIFY`, `GO-VERIFY`, and `RELEASE-VERIFY`; JavaScript remains a graph protocol reference while Go additionally covers intake and workflow behavior. Proposal validation and ready status are stored as independent evidence. Node's TAP output and Go's JSON test stream are also recorded separately. The release node binds the declared target list to an inspected binary report. Any shared contract, fixture, covered implementation, or target change makes the relevant proof stale until that check is rerun and re-attested.
