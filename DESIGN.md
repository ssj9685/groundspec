# Design

## D-01: Deterministic artifact core

Requirements: PB-01, PB-02, PB-03, PB-04, PB-07, PB-08, PB-09, PB-10, PB-11, PB-16, PB-17, PB-20, PB-23, PB-24, PB-31

Choice: Keep parsing, canonical serialization, digest binding, validation, status calculation, materialization, and initialization in focused Go standard-library packages under the existing source tree.

Rationale: This preserves deterministic behavior, portability, and the approved single-CLI architecture.

## D-02: External adapter boundary

Requirements: PB-05, PB-12, PB-14, PB-15, PB-18

Choice: Define versioned file contracts for proposals, plans, and implementation results; isolate argv-based agent CLI discovery and invocation in the existing external agent adapter package.

Rationale: Providers remain replaceable and provider SDKs do not enter the core.

## D-03: Lifecycle graph and verification

Requirements: PB-06, PB-13, PB-19, PB-21, PB-22, PB-25, PB-26, PB-27, PB-28

Choice: Validate explicit contract links, generate bounded context and impact artifacts, and require fresh graph proof when .groundspec/graph.json exists or --graph is supplied; retain non-graph lifecycle behavior for unadopted workspaces.

Rationale: This supplies auditable completion evidence without breaking existing workspaces.

## D-04: Publication and release boundaries

Requirements: PB-29, PB-30

Choice: Keep publication auditing and release assembly as explicit CLI actions using allowlists, fail-closed validation, Go cross-builds, and checksum generation.

Rationale: Separating publication from implementation limits disclosure risk and keeps releases self-contained.
