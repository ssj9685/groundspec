# Proposal adapter boundary

A proposal adapter is a separate program that reads a current `SourceBundle` and writes a requirement proposal matching `schemas/requirement-proposal.schema.json`.

It may be a Codex task, another hosted model, a local model, or a human-authored script. `groundspec` does not load the adapter, execute a command from repository configuration, or require provider-specific fields.

## Contract

The adapter must:

1. preserve the repository-relative SourceBundle path;
2. record the SHA-256 digest of the exact SourceBundle file bytes;
3. identify itself with a non-empty producer name and version;
4. classify candidates as `explicit`, `inferred`, or `assumption`;
5. cite one or more existing block IDs for every requirement and question;
6. emit candidates only—the adapter never records human approval.

Provider, model, prompt, and sampling details may be recorded in a separate adapter-run artifact and included in the repository graph when a project needs reproducibility or an AI-usage report. They are deliberately not required by the portable proposal protocol.

## Example flow

```text
groundspec ingest source.md --json
            │
            ▼
.groundspec/sources/source.json
            │
            ▼
external adapter
            │
            ▼
.groundspec/proposal.json
            │
            ├─ groundspec proposal validate
            ├─ groundspec review
            └─ groundspec status
```

The project chooses how to invoke the external adapter. The core starts from the saved proposal, validates that the SourceBundle is still the exact deterministic ingestion result, and keeps review decisions in a separate file. Reviews bind to the deterministic review surface rather than producer metadata or unrelated source bytes, so only changes a reviewer actually needs to reconsider invalidate decisions.

`ready` means all human decisions exist. A later materializer adapter may consume the proposal plus review to draft `SPEC.md`, `TEST_SPEC.md`, and `DESIGN.md`; those drafts still remain normal graph inputs subject to tests and evidence.
