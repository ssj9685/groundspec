# Reviewed workflow lifecycle

This contract makes the terminal boundary explicit: targeted impact checks can guide work, but only the reviewed full suite and fresh graph evidence can complete a lifecycle.

```mermaid
stateDiagram-v2
  %% @name GroundSpecLifecycle
  [*] --> SourceReady
  SourceReady --> ProposalReady : propose [sourceCurrent] / recordProposal
  ProposalReady --> ReviewReady : review [allCandidatesDecided] / recordReview
  ReviewReady --> PlanReady : materialize [projectContextCurrent] / recordPlan
  PlanReady --> Implemented : implement [explicitAction] / recordImplementation
  Implemented --> TargetedVerified : impactCheck / recordTargetedEvidence
  TargetedVerified --> FullyVerified : verify [reviewedArgvOnly] / recordFullVerification
  FullyVerified --> EvidenceCurrent : attest [fullSuitePassed] / recordGraphEvidence
  EvidenceCurrent --> Complete : complete [graphCurrent] / recordCompletion
  Complete --> [*]

  %% @test SourceReady --propose--> ProposalReady
  %% @test ProposalReady --review--> ReviewReady
  %% @test ReviewReady --materialize--> PlanReady
  %% @test PlanReady --implement--> Implemented
  %% @test Implemented --impactCheck--> TargetedVerified
  %% @test TargetedVerified --complete--> !invalid
  %% @test TargetedVerified --verify--> FullyVerified
  %% @test FullyVerified --attest--> EvidenceCurrent
  %% @test EvidenceCurrent --complete--> Complete
```
