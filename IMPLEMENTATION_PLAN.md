# Implementation Plan

Language: Go
Framework: Go standard library
Source directory: `.`

## Tasks

- `I-01` (PB-01, PB-02, PB-03, PB-04, PB-07, PB-20): Complete format-neutral local intake and canonical SourceBundle generation, adding bounded text-bearing PDF extraction while keeping OCR and network access outside the core.
- `I-02` (PB-05, PB-08, PB-09, PB-10, PB-11, PB-12, PB-23, PB-24): Complete versioned proposal, review, status, and project-context validation with exact provenance and candidate-digest binding and no provider SDK dependencies.
- `I-03` (PB-14, PB-15, PB-16, PB-17, PB-18, PB-31): Complete safe argv-based agent discovery and selection, non-destructive project initialization, reviewed plan materialization, and explicit implementation-result recording through the existing adapter boundary.
- `I-04` (PB-06, PB-13, PB-19, PB-21, PB-22, PB-25, PB-26, PB-27, PB-28): Complete explicit contract-link validation, bounded impact context, dogfood regression promotion, reviewed full verification, optional graph adoption, fresh proof gating, and exact mermaid-spec pin verification.
- `I-05` (PB-29, PB-30): Complete fail-closed publication auditing and portable MIT release assembly with stable Go installation, self-contained versioned binaries, checksums, and CI source-build verification.
- `I-06` (PB-01, PB-02, PB-03, PB-04, PB-05, PB-06, PB-07, PB-08, PB-09, PB-10, PB-11, PB-12, PB-13, PB-14, PB-15, PB-16, PB-17, PB-18, PB-19, PB-20, PB-21, PB-22, PB-23, PB-24, PB-25, PB-26, PB-27, PB-28, PB-29, PB-30, PB-31): Add or update deterministic unit, contract, integration, conformance, and regression fixtures so every accepted requirement is exercised through the existing Go CLI implementation.

## Verification commands

- `V-01` (required) in `.`: `go test ./...`
- `V-02` (required) in `.`: `go vet ./...`
- `V-03` (required) in `.`: `go build ./cmd/groundspec`
- `V-04` (required) in `.`: `bun run scripts/verify-contract-pin.js`
- `V-05` (required) in `.`: `bun run scripts/contracts-verify.js`
- `V-06` (required) in `.`: `bun run scripts/contracts-impact.smoke.js`
- `V-07` (required) in `.`: `bun run scripts/publication-audit.js`
- `V-08` (required) in `.`: `npm test`
