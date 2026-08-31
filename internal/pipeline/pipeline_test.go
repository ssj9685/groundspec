package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ssj9685/groundspec/internal/agent"
	"github.com/ssj9685/groundspec/internal/intake"
	"github.com/ssj9685/groundspec/internal/workflow"
)

type fixtureAdapter struct {
	draft []byte
	plan  []byte
	root  string
	calls int
}

type sequenceAdapter struct {
	outputs  [][]byte
	requests []agent.Request
}

func publicTestDescriptor() agent.Descriptor {
	return agent.Descriptor{ID: "fixture", Name: "Fixture", Kind: "test", Version: "1", Auth: "authenticated", Ready: true}
}

func (adapter *sequenceAdapter) Descriptor() agent.Descriptor {
	return agent.Descriptor{ID: "sequence", Name: "Sequence", Kind: "test", Version: "1", Auth: "test", Ready: true}
}

func (adapter *sequenceAdapter) Run(_ context.Context, request agent.Request) ([]byte, error) {
	adapter.requests = append(adapter.requests, request)
	return adapter.outputs[len(adapter.requests)-1], nil
}

func TestPB16DraftRejectsStaleBundleBeforeCallingAdapter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "brief.md"), []byte("# Original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := intake.Ingest(root, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(root, ".groundspec/sources/brief.json", bundle); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "brief.md"), []byte("# Changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &fixtureAdapter{draft: []byte(`{"version":1,"requirements":[],"questions":[]}`)}
	_, err = Draft(context.Background(), root, ".groundspec/sources/brief.json", ".groundspec/proposal.json", adapter)
	if err == nil || !strings.Contains(err.Error(), "current deterministic ingestion result") {
		t.Fatalf("stale bundle error = %v", err)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter called %d times for a stale bundle", adapter.calls)
	}
}

func TestPB23RedraftRejectsRewritingCandidatesFromUnchangedBlocks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "brief.md"), []byte("# Brief\n\nExisting behavior.\n\nNew behavior.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := intake.Ingest(root, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := ".groundspec/sources/brief.json"
	if err := writeJSONAtomic(root, bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	bundleBytes, err := readInside(root, bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var existingBlock, newBlock string
	for _, block := range bundle.Blocks {
		switch block.Text {
		case "Existing behavior.":
			existingBlock = block.ID
		case "New behavior.":
			newBlock = block.ID
		}
	}
	if existingBlock == "" || newBlock == "" {
		t.Fatalf("fixture blocks missing: %#v", bundle.Blocks)
	}
	original := workflow.Requirement{ID: "R-01", Statement: "Existing behavior.", Classification: "explicit", SourceBlocks: []string{existingBlock}}
	previous := workflow.Proposal{
		Version:      protocolVersion,
		SourceBundle: workflow.ArtifactReference{Path: bundlePath, Digest: digest(bundleBytes)},
		Producer:     workflow.Producer{Name: "fixture", Version: "1"},
		Requirements: []workflow.Requirement{original},
		Questions:    []workflow.Question{},
	}
	proposalPath := ".groundspec/proposal.json"
	if err := writeJSONAtomic(root, proposalPath, previous); err != nil {
		t.Fatal(err)
	}
	rewritten, _ := json.Marshal(draftOutput{Version: 1, Requirements: []workflow.Requirement{
		{ID: "R-01", Statement: "Rewritten behavior.", Classification: "explicit", SourceBlocks: []string{existingBlock}},
		{ID: "R-02", Statement: "New behavior.", Classification: "explicit", SourceBlocks: []string{newBlock}},
	}, Questions: []workflow.Question{}})
	corrected, _ := json.Marshal(draftOutput{Version: 1, Requirements: []workflow.Requirement{
		original,
		{ID: "R-02", Statement: "New behavior.", Classification: "explicit", SourceBlocks: []string{newBlock}},
	}, Questions: []workflow.Question{}})
	adapter := &sequenceAdapter{outputs: [][]byte{rewritten, corrected}}
	proposal, err := Draft(context.Background(), root, bundlePath, proposalPath, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.requests) != 2 || !reflect.DeepEqual(proposal.Requirements[0], original) {
		t.Fatalf("redraft did not preserve the current candidate: calls=%d proposal=%#v", len(adapter.requests), proposal)
	}
	if _, err := os.Stat(filepath.Join(root, ".groundspec/rejections/development-plan-latest.json")); err == nil {
		t.Fatal("proposal rejection overwrote the development-plan rejection path")
	}
	if _, err := os.Stat(filepath.Join(root, ".groundspec/rejections/proposal-latest.json")); err != nil {
		t.Fatalf("missing rejected redraft evidence: %v", err)
	}
}

func (adapter *fixtureAdapter) Descriptor() agent.Descriptor {
	return agent.Descriptor{ID: "fixture", Name: "Fixture", Kind: "test", Version: "1", Auth: "test", Ready: true}
}

func (adapter *fixtureAdapter) Run(_ context.Context, _ agent.Request) ([]byte, error) {
	adapter.calls++
	switch adapter.calls {
	case 1:
		return adapter.draft, nil
	case 2:
		return adapter.plan, nil
	default:
		if err := os.MkdirAll(filepath.Join(adapter.root, "source-code"), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(adapter.root, "source-code", "app.txt"), []byte("runnable\n"), 0o644); err != nil {
			return nil, err
		}
		return []byte(`{"summary":"implemented fixture","files":["source-code/app.txt"],"checks":[]}`), nil
	}
}

func TestPB16ThroughPB21TerminalLifecycleAndFreshness(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "brief.md")
	if err := os.WriteFile(source, []byte("# Brief\n\nThe product writes a runnable artifact.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previewRoot := filepath.Join(parent, "preview")
	if err := os.MkdirAll(previewRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previewRoot, "brief.md"), []byte("# Brief\n\nThe product writes a runnable artifact.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := intake.Ingest(previewRoot, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	blockID := bundle.Blocks[len(bundle.Blocks)-1].ID

	draft, _ := json.Marshal(draftOutput{Version: 1, Requirements: []workflow.Requirement{{ID: "R-01", Statement: "The product writes a runnable artifact.", Classification: "explicit", SourceBlocks: []string{blockID}}}, Questions: []workflow.Question{}})
	plan, _ := json.Marshal(planOutput{
		Version: 1, Project: "fixture", Summary: "A minimal runnable fixture.",
		Requirements:   []PlanRequirement{{ID: "R-01", Title: "Runnable artifact", Behavior: "Write one artifact.", AcceptanceCriteria: []string{"The artifact exists."}}},
		Design:         []DesignDecision{{ID: "D-01", Title: "Plain file", RequirementIDs: []string{"R-01"}, Choice: "Use a text file.", Rationale: "It is the smallest runnable fixture."}},
		Tests:          []TestCase{{ID: "T-01", RequirementIDs: []string{"R-01"}, Level: "integration", Given: "an implemented workspace", When: "verification runs", Then: "the artifact exists"}},
		Implementation: ImplementationPlan{Language: "text", Framework: "none", SourceDirectory: "source-code", Tasks: []Task{{ID: "I-01", RequirementIDs: []string{"R-01"}, Description: "Write the artifact."}}, VerificationCommands: []VerificationCommand{{ID: "V-01", WorkingDirectory: "source-code", Argv: []string{"test", "-f", "app.txt"}}}},
	})
	root := filepath.Join(parent, "workspace")
	adapter := &fixtureAdapter{draft: draft, plan: plan, root: root}
	workspace, err := Start(context.Background(), source, root, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Adapter != "fixture" {
		t.Fatalf("workspace = %#v", workspace)
	}
	if _, err := os.Stat(filepath.Join(root, workspace.Review)); err != nil {
		t.Fatalf("bootstrap did not create the review gate: %v", err)
	}
	bootstrapStatus, err := workflow.Status(root, workspace.Proposal, workspace.Review)
	if err != nil {
		t.Fatal(err)
	}
	if bootstrapStatus.State != "blocked" || bootstrapStatus.Next != "review" {
		t.Fatalf("bootstrap status = %#v", bootstrapStatus)
	}
	if _, err := workflow.ApplyReview(root, workspace.Proposal, workspace.Review, workflow.ReviewAction{Kind: "accept", ID: "R-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(context.Background(), root, workspace.Proposal, workspace.Review, ".groundspec/development-plan.json", adapter); err != nil {
		t.Fatal(err)
	}
	if _, err := Implement(context.Background(), root, ".groundspec/development-plan.json", ".groundspec/implementation-result.json", adapter); err != nil {
		t.Fatal(err)
	}
	verification, err := Verify(context.Background(), root, ".groundspec/development-plan.json", ".groundspec/implementation-result.json", ".groundspec/verification.json")
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Passed {
		t.Fatalf("verification = %#v", verification)
	}
	complete := Lifecycle(root, workspace.Proposal, workspace.Review, ".groundspec/development-plan.json", ".groundspec/implementation-result.json", ".groundspec/verification.json")
	if complete.State != "complete" {
		t.Fatalf("lifecycle = %#v", complete)
	}
	tamperedVerification := verification
	tamperedVerification.Commands = []CommandResult{}
	if err := writeJSONAtomic(root, ".groundspec/verification.json", tamperedVerification); err != nil {
		t.Fatal(err)
	}
	incomplete := Lifecycle(root, workspace.Proposal, workspace.Review, ".groundspec/development-plan.json", ".groundspec/implementation-result.json", ".groundspec/verification.json")
	if incomplete.State != "blocked" || incomplete.Next != "verification" {
		t.Fatalf("incomplete verification lifecycle = %#v", incomplete)
	}
	if _, err := Verify(context.Background(), root, ".groundspec/development-plan.json", ".groundspec/implementation-result.json", ".groundspec/verification.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source-code", "unrecorded.txt"), []byte("new artifact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := Lifecycle(root, workspace.Proposal, workspace.Review, ".groundspec/development-plan.json", ".groundspec/implementation-result.json", ".groundspec/verification.json")
	if stale.State != "blocked" || stale.Next != "implementation" {
		t.Fatalf("stale lifecycle = %#v", stale)
	}
}

func TestPB31InitPreservesAnExistingProjectAndRejectsReservedPaths(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "brief.md")
	if err := os.WriteFile(source, []byte("# Brief\n\nKeep the existing project intact.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := filepath.Join(parent, "preview")
	if err := os.MkdirAll(preview, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preview, "brief.md"), []byte("# Brief\n\nKeep the existing project intact.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := intake.Ingest(preview, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := json.Marshal(draftOutput{Version: 1, Requirements: []workflow.Requirement{{ID: "R-01", Statement: "Keep the existing project intact.", Classification: "explicit", SourceBlocks: []string{bundle.Blocks[len(bundle.Blocks)-1].ID}}}, Questions: []workflow.Question{}})

	root := filepath.Join(parent, "existing")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	existingPath := filepath.Join(root, "package.json")
	if err := os.WriteFile(existingPath, []byte("{\"private\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &fixtureAdapter{draft: draft, root: root}
	workspace, err := Init(context.Background(), source, root, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Source != ".groundspec/source-material/brief.md" {
		t.Fatalf("source path = %q", workspace.Source)
	}
	if contents, err := os.ReadFile(existingPath); err != nil || string(contents) != "{\"private\":true}\n" {
		t.Fatalf("existing project file changed: %q, %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(root, workspace.Proposal)); err != nil {
		t.Fatalf("workflow proposal missing: %v", err)
	}
	if _, err := Init(context.Background(), source, root, adapter); err == nil || !strings.Contains(err.Error(), "reserved GroundSpec path") {
		t.Fatalf("second initialization did not fail closed: %v", err)
	}
}

func TestPB31InitRejectsMaterializationTargetsBeforeWriting(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "brief.md")
	if err := os.WriteFile(source, []byte("# Brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "existing")
	reserved := filepath.Join(root, "test-or-verification", "TEST_SPEC.md")
	if err := os.MkdirAll(filepath.Dir(reserved), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("existing verification contract\n")
	if err := os.WriteFile(reserved, original, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Init(context.Background(), source, root, nil)
	if err == nil || !strings.Contains(err.Error(), "test-or-verification/TEST_SPEC.md") {
		t.Fatalf("reserved materialization target was not rejected: %v", err)
	}
	if contents, readErr := os.ReadFile(reserved); readErr != nil || !reflect.DeepEqual(contents, original) {
		t.Fatalf("reserved file changed: %q, %v", contents, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".groundspec")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("initialization wrote workflow artifacts before rejecting the conflict: %v", statErr)
	}
}

func TestPB24PlanCannotReplaceApprovedArchitecture(t *testing.T) {
	root := t.TempDir()
	context := ProjectContext{Version: 1, Project: "tool", Language: "Go", Framework: "Go standard library", SourceDirectory: ".", Constraints: []string{"single binary"}}
	if err := writeJSONAtomic(root, ".groundspec/project-context.json", context); err != nil {
		t.Fatal(err)
	}
	contextBytes, err := readInside(root, ".groundspec/project-context.json")
	if err != nil {
		t.Fatal(err)
	}
	accepted := []workflow.Requirement{{ID: "R-01"}}
	plan := DevelopmentPlan{
		Version: 1, Adapter: publicTestDescriptor(), Project: "tool", Summary: "existing tool", ProjectContext: &Reference{Path: ".groundspec/project-context.json", Digest: digest(contextBytes)},
		Requirements:   []PlanRequirement{{ID: "R-01", Title: "Keep architecture", Behavior: "Preserve it.", AcceptanceCriteria: []string{"It stays."}}},
		Tests:          []TestCase{{ID: "T-01", RequirementIDs: []string{"R-01"}, Level: "unit", Given: "Go", When: "planned", Then: "Go remains"}},
		Implementation: ImplementationPlan{Language: "Python", Framework: "standard library", SourceDirectory: ".", Tasks: []Task{{ID: "I-01", RequirementIDs: []string{"R-01"}, Description: "Replace it."}}, VerificationCommands: []VerificationCommand{{ID: "V-01", WorkingDirectory: ".", Argv: []string{"python", "-V"}}}},
	}
	if err := ValidatePlan(root, plan, accepted); err == nil {
		t.Fatal("architecture replacement should be rejected")
	}
	plan.Implementation.Language = "Go"
	plan.Implementation.Framework = "Go standard library"
	if err := ValidatePlan(root, plan, accepted); err != nil {
		t.Fatalf("preserved architecture rejected: %v", err)
	}
	plan.ProjectContext = nil
	if err := ValidatePlan(root, plan, accepted); err == nil {
		t.Fatal("plan without the reviewed project-context binding was accepted")
	}
	plan.ProjectContext = &Reference{Path: ".groundspec/project-context.json", Digest: digest(contextBytes)}
	plan.Project = "replacement"
	if err := ValidatePlan(root, plan, accepted); err == nil {
		t.Fatal("plan with a replaced project identity was accepted")
	}
}

func TestPB24ProjectContextIsStrictAndSymlinkSafe(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".groundspec"), 0o755); err != nil {
		t.Fatal(err)
	}
	contextPath := filepath.Join(root, ".groundspec", "project-context.json")
	if err := os.WriteFile(contextPath, []byte(`{"version":1,"project":"tool","language":"Go","framework":"stdlib","sourceDirectory":".","constraints":[],"extra":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadProjectContext(root, ".groundspec/project-context.json"); err == nil {
		t.Fatal("project context with an unknown field was accepted")
	}

	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("cannot create symlink fixture: %v", err)
	}
	if _, err := inside(root, "linked/output.txt"); err == nil {
		t.Fatal("project path traversing a symlink was accepted")
	}
}

func TestPB19PlanRejectsShellFormVerificationCommands(t *testing.T) {
	root := t.TempDir()
	accepted := []workflow.Requirement{{ID: "R-01"}}
	plan := DevelopmentPlan{
		Version: 1, Adapter: publicTestDescriptor(), Project: "tool", Summary: "reviewed argv only",
		Requirements: []PlanRequirement{{ID: "R-01", Title: "Verify", Behavior: "Run reviewed argv.", AcceptanceCriteria: []string{"It runs directly."}}},
		Tests:        []TestCase{{ID: "T-01", RequirementIDs: []string{"R-01"}, Level: "integration", Given: "a plan", When: "validated", Then: "shell forms fail"}},
		Implementation: ImplementationPlan{
			Language: "Go", Framework: "Go standard library", SourceDirectory: ".",
			Tasks:                []Task{{ID: "I-01", RequirementIDs: []string{"R-01"}, Description: "Implement direct verification."}},
			VerificationCommands: []VerificationCommand{{ID: "V-01", WorkingDirectory: ".", Argv: []string{"go", "test", "./..."}}},
		},
	}
	if err := ValidatePlan(root, plan, accepted); err != nil {
		t.Fatalf("direct argv rejected: %v", err)
	}
	for name, argv := range map[string][]string{
		"command string": {"go test ./..."},
		"shell":          {"/bin/sh", "-c", "go test ./..."},
		"powershell":     {"pwsh", "-Command", "go test ./..."},
		"env wrapper":    {"/usr/bin/env", "sh", "-c", "go test ./..."},
	} {
		t.Run(name, func(t *testing.T) {
			plan.Implementation.VerificationCommands[0].Argv = argv
			if err := ValidatePlan(root, plan, accepted); err == nil {
				t.Fatalf("shell-form argv was accepted: %#v", argv)
			}
		})
	}
	plan.Implementation.VerificationCommands[0].Argv = []string{"go", "test", "./..."}
	plan.Implementation.VerificationCommands[0].ID = "../escape"
	if err := ValidatePlan(root, plan, accepted); err == nil {
		t.Fatal("path-like verification id was accepted")
	}
}

func TestPB29PlanRejectsRuntimeAdapterProvenance(t *testing.T) {
	root := t.TempDir()
	accepted := []workflow.Requirement{{ID: "R-01"}}
	plan := DevelopmentPlan{
		Version: 1, Adapter: publicTestDescriptor(), Project: "tool", Summary: "portable provenance",
		Requirements: []PlanRequirement{{ID: "R-01", Title: "Portable", Behavior: "Keep adapter provenance portable.", AcceptanceCriteria: []string{"No local details remain."}}},
		Tests:        []TestCase{{ID: "T-01", RequirementIDs: []string{"R-01"}, Level: "unit", Given: "a plan", When: "validated", Then: "local details fail"}},
		Implementation: ImplementationPlan{
			Language: "Go", Framework: "Go standard library", SourceDirectory: ".",
			Tasks:                []Task{{ID: "I-01", RequirementIDs: []string{"R-01"}, Description: "Validate provenance."}},
			VerificationCommands: []VerificationCommand{{ID: "V-01", WorkingDirectory: ".", Argv: []string{"go", "test", "./..."}}},
		},
	}
	if err := ValidatePlan(root, plan, accepted); err != nil {
		t.Fatalf("portable descriptor rejected: %v", err)
	}
	plan.Adapter.Executable = "/opt/example/bin/codex"
	if err := ValidatePlan(root, plan, accepted); err == nil {
		t.Fatal("plan containing a local adapter executable was accepted")
	}
}

func TestPB29ImplementationRecordRejectsRuntimeAdapterProvenance(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifacts, err := inventory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	plan := DevelopmentPlan{Version: 1, Implementation: ImplementationPlan{SourceDirectory: "."}}
	planBytes := []byte("reviewed plan")
	record := ImplementationResult{
		Version:    1,
		Plan:       Reference{Path: ".groundspec/development-plan.json", Digest: digest(planBytes)},
		Adapter:    publicTestDescriptor(),
		Summary:    "implemented",
		Artifacts:  artifacts,
		Changes:    []ArtifactChange{},
		Checks:     []ReportedCheck{},
		RecordedAt: "2026-09-01T00:00:00Z",
	}
	if err := writeJSONAtomic(root, ".groundspec/implementation-result.json", record); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCurrentImplementation(root, record.Plan.Path, plan, planBytes, ".groundspec/implementation-result.json"); err != nil {
		t.Fatalf("portable implementation record rejected: %v", err)
	}

	record.Adapter.Auth = "Logged in using ChatGPT"
	if err := writeJSONAtomic(root, ".groundspec/implementation-result.json", record); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCurrentImplementation(root, record.Plan.Path, plan, planBytes, ".groundspec/implementation-result.json"); err == nil {
		t.Fatal("implementation record containing login detail was accepted")
	}
}

func TestPB18ImplementationInventoryIncludesReleaseMetadataAndExcludesPrivateGeneratedTrees(t *testing.T) {
	root := t.TempDir()
	for path, contents := range map[string]string{
		"main.go":                                "package main\n",
		".github/workflows/ci.yml":               "name: CI\n",
		".groundspec/plan.json":                  "{}\n",
		".dogfood-private/secret.txt":            "private\n",
		"test-or-verification/commands/test.log": "passed\n",
		"dist/app.js":                            "built\n",
		"node_modules/pkg/index.js":              "dependency\n",
		"submission.zip":                         "derived archive\n",
		"submission.tar.gz":                      "derived archive\n",
	} {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	artifacts, err := inventory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/workflows/ci.yml", "main.go"}
	if len(artifacts) != len(want) {
		t.Fatalf("inventory = %#v", artifacts)
	}
	for index, path := range want {
		if artifacts[index].Path != path {
			t.Fatalf("artifact %d = %q, want %q", index, artifacts[index].Path, path)
		}
	}
}

func TestOptionalVerificationFailureIsEvidenceButNotCompletionBlocker(t *testing.T) {
	root := t.TempDir()
	log := []byte("optional browser is not installed\n")
	if err := writeAtomic(root, "test-or-verification/commands/WEBKIT.log", log); err != nil {
		t.Fatal(err)
	}
	plan := DevelopmentPlan{Version: 1, Implementation: ImplementationPlan{VerificationCommands: []VerificationCommand{{ID: "WEBKIT", WorkingDirectory: ".", Argv: []string{"webkit-check"}, Optional: true}}}}
	planBytes := []byte("plan")
	implementationBytes := []byte("implementation")
	verification := VerificationResult{
		Version:        1,
		Plan:           Reference{Path: "plan.json", Digest: digest(planBytes)},
		Implementation: Reference{Path: "implementation.json", Digest: digest(implementationBytes)},
		Passed:         true,
		RecordedAt:     "2026-09-01T00:00:00Z",
		Commands:       []CommandResult{{ID: "WEBKIT", WorkingDirectory: ".", Argv: []string{"webkit-check"}, ExitCode: 1, Passed: false, Optional: true, Log: Artifact{Path: "test-or-verification/commands/WEBKIT.log", Digest: digest(log)}}},
	}
	if err := validateVerificationRecord(root, "plan.json", plan, planBytes, "implementation.json", implementationBytes, verification); err != nil {
		t.Fatalf("optional failure should remain valid evidence: %v", err)
	}
}

func TestPB28TargetedImpactEvidenceCannotReplaceFullVerification(t *testing.T) {
	root := t.TempDir()
	targetedLog := []byte("targeted impact tests passed\n")
	if err := writeAtomic(root, "test-or-verification/commands/TARGETED.log", targetedLog); err != nil {
		t.Fatal(err)
	}
	plan := DevelopmentPlan{Version: 1, Implementation: ImplementationPlan{VerificationCommands: []VerificationCommand{
		{ID: "TARGETED", WorkingDirectory: ".", Argv: []string{"go", "test", "./internal/pipeline"}},
		{ID: "FULL", WorkingDirectory: ".", Argv: []string{"go", "test", "./..."}},
	}}}
	planBytes := []byte("plan")
	implementationBytes := []byte("implementation")
	targetedOnly := VerificationResult{
		Version:        1,
		Plan:           Reference{Path: "plan.json", Digest: digest(planBytes)},
		Implementation: Reference{Path: "implementation.json", Digest: digest(implementationBytes)},
		Passed:         true,
		RecordedAt:     "2026-09-01T00:00:00Z",
		Commands: []CommandResult{{
			ID: "TARGETED", WorkingDirectory: ".", Argv: []string{"go", "test", "./internal/pipeline"}, ExitCode: 0, Passed: true,
			Log: Artifact{Path: "test-or-verification/commands/TARGETED.log", Digest: digest(targetedLog)},
		}},
	}
	if err := validateVerificationRecord(root, "plan.json", plan, planBytes, "implementation.json", implementationBytes, targetedOnly); err == nil {
		t.Fatal("targeted impact evidence incorrectly satisfied full verification")
	}
}
