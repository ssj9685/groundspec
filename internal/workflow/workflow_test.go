package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ssj9685/groundspec/internal/intake"
)

type workflowFixture struct {
	root         string
	proposalPath string
	reviewPath   string
	requirement  string
	question     string
}

func writeFixtureJSON(t *testing.T, path string, value any) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func rawDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func newWorkflowFixture(t *testing.T) workflowFixture {
	t.Helper()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "brief.md")
	if err := os.WriteFile(sourcePath, []byte("# Brief\n\n- Works offline.\n- How should retries work?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := intake.Ingest(root, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := ".groundspec/sources/brief.json"
	bundleBytes := writeFixtureJSON(t, filepath.Join(root, bundlePath), bundle)
	proposalPath := ".groundspec/proposal.json"
	proposal := Proposal{
		Version: 1,
		SourceBundle: ArtifactReference{
			Path:   bundlePath,
			Digest: rawDigest(bundleBytes),
		},
		Producer: Producer{Name: "fixture-adapter", Version: "1.0.0"},
		Requirements: []Requirement{
			{ID: "R-01", Statement: "The product works offline.", Classification: "explicit", SourceBlocks: []string{bundle.Blocks[1].ID}},
		},
		Questions: []Question{
			{ID: "Q-01", Question: "How should retries work?", SourceBlocks: []string{bundle.Blocks[2].ID}},
		},
	}
	writeFixtureJSON(t, filepath.Join(root, proposalPath), proposal)
	return workflowFixture{
		root:         root,
		proposalPath: proposalPath,
		reviewPath:   ".groundspec/review.json",
		requirement:  "R-01",
		question:     "Q-01",
	}
}

func readFixtureProposal(t *testing.T, fixture workflowFixture) Proposal {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixture.root, fixture.proposalPath))
	if err != nil {
		t.Fatal(err)
	}
	var proposal Proposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestTV16ProposalValidationIsDeterministicAndProviderNeutral(t *testing.T) {
	fixture := newWorkflowFixture(t)
	first, err := ValidateProposal(fixture.root, fixture.proposalPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidateProposal(fixture.root, fixture.proposalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Report, second.Report) {
		t.Fatalf("reports differ:\nfirst=%+v\nsecond=%+v", first.Report, second.Report)
	}
	if first.Report.Requirements != 1 || first.Report.Questions != 1 || first.Report.Producer.Name != "fixture-adapter" {
		t.Fatalf("report = %+v", first.Report)
	}

	proposal := readFixtureProposal(t, fixture)
	proposal.Producer = Producer{Name: "different-provider", Version: "2026-09"}
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
	if _, err := ValidateProposal(fixture.root, fixture.proposalPath); err != nil {
		t.Fatalf("provider-neutral proposal was rejected: %v", err)
	}
}

func TestTV17ProposalRejectsStaleSourcesAndInvalidReferences(t *testing.T) {
	t.Run("source changed", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		if err := os.WriteFile(filepath.Join(fixture.root, "brief.md"), []byte("# Changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateProposal(fixture.root, fixture.proposalPath); err == nil || !strings.Contains(err.Error(), "current ingestion result") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("bundle digest changed", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		proposal := readFixtureProposal(t, fixture)
		proposal.SourceBundle.Digest = strings.Repeat("0", 64)
		writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
		if _, err := ValidateProposal(fixture.root, fixture.proposalPath); err == nil || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		proposal := readFixtureProposal(t, fixture)
		proposal.Questions[0].ID = proposal.Requirements[0].ID
		writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
		if _, err := ValidateProposal(fixture.root, fixture.proposalPath); err == nil || !strings.Contains(err.Error(), "duplicate id") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unknown block", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		proposal := readFixtureProposal(t, fixture)
		proposal.Requirements[0].SourceBlocks = []string{"B-000000000000"}
		writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
		if _, err := ValidateProposal(fixture.root, fixture.proposalPath); err == nil || !strings.Contains(err.Error(), "missing source block") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestTV18ReviewIsSeparateAndBoundToProposal(t *testing.T) {
	fixture := newWorkflowFixture(t)
	review, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "accept", ID: fixture.requirement, Text: "Confirmed from the brief.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Decisions) != 1 || review.Decisions[0].Decision != "accepted" || review.Proposal.Path != fixture.proposalPath {
		t.Fatalf("review = %+v", review)
	}
	validated, err := ValidateProposal(fixture.root, fixture.proposalPath)
	if err != nil {
		t.Fatal(err)
	}
	if review.Decisions[0].CandidateDigest != candidateDigestForRequirement(validated.Proposal.Requirements[0]) {
		t.Fatalf("candidate digest = %s", review.Decisions[0].CandidateDigest)
	}

	review, err = ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "resolve", ID: fixture.question, Text: "Retry once after an explicit user action.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Resolutions) != 1 || review.Resolutions[0].ID != fixture.question {
		t.Fatalf("review = %+v", review)
	}
}

func TestTV19ReviewRejectsWrongKindsWithoutWriting(t *testing.T) {
	fixture := newWorkflowFixture(t)
	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "accept", ID: fixture.question,
	}); err == nil || !strings.Contains(err.Error(), "not a requirement") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, fixture.reviewPath)); !os.IsNotExist(err) {
		t.Fatalf("invalid action wrote a review: %v", err)
	}

	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "accept", ID: fixture.requirement,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPB10ReviewRejectsUnknownCandidateIDs(t *testing.T) {
	for name, review := range map[string]Review{
		"decision": {
			Version:     1,
			Decisions:   []Decision{{ID: "R-UNKNOWN", CandidateDigest: strings.Repeat("0", 64), Decision: "accepted"}},
			Resolutions: []Resolution{},
		},
		"resolution": {
			Version:     1,
			Decisions:   []Decision{},
			Resolutions: []Resolution{{ID: "Q-UNKNOWN", CandidateDigest: strings.Repeat("0", 64), Answer: "Unknown."}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			review.Proposal = ReviewReference{Path: fixture.proposalPath}
			writeFixtureJSON(t, filepath.Join(fixture.root, fixture.reviewPath), review)
			if _, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath); err == nil || !strings.Contains(err.Error(), "not a") {
				t.Fatalf("unknown review candidate error = %v", err)
			}
		})
	}
}

func TestTV22ReviewSurvivesProviderMetadataChanges(t *testing.T) {
	fixture := newWorkflowFixture(t)
	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "accept", ID: fixture.requirement,
	}); err != nil {
		t.Fatal(err)
	}
	proposal := readFixtureProposal(t, fixture)
	proposal.Producer = Producer{Name: "replacement-adapter", Version: "2.0.0"}
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
	status, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.Summary.Accepted != 1 || status.Summary.Unresolved != 1 {
		t.Fatalf("status = %+v", status)
	}

	if err := os.WriteFile(filepath.Join(fixture.root, "brief.md"), []byte("# Brief\n\n- Works offline.\n- How should retries work?\n\nUnrelated note.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := intake.Ingest(fixture.root, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	bundleBytes := writeFixtureJSON(t, filepath.Join(fixture.root, proposal.SourceBundle.Path), bundle)
	proposal.SourceBundle.Digest = rawDigest(bundleBytes)
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
	status, err = Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.Summary.Accepted != 1 || status.Summary.Unresolved != 1 {
		t.Fatalf("status after unrelated source change = %+v", status)
	}
}

func TestTV20TV21StatusListsBlockersThenBecomesReady(t *testing.T) {
	fixture := newWorkflowFixture(t)
	blocked, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.State != "blocked" || blocked.Next != "review" || len(blocked.Blockers) != 2 {
		t.Fatalf("blocked status = %+v", blocked)
	}
	if blocked.Blockers[0].ID != fixture.requirement || blocked.Blockers[1].ID != fixture.question {
		t.Fatalf("blocker order = %+v", blocked.Blockers)
	}

	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{Kind: "reject", ID: fixture.requirement}); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{Kind: "resolve", ID: fixture.question, Text: "No automatic retries."}); err != nil {
		t.Fatal(err)
	}
	ready, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if ready.State != "ready" || ready.Next != "materialize" || ready.Summary.Rejected != 1 || ready.Summary.Resolved != 1 || len(ready.Blockers) != 0 {
		t.Fatalf("ready status = %+v", ready)
	}
	if !reflect.DeepEqual(ready, again) {
		t.Fatalf("status is not deterministic:\nfirst=%+v\nsecond=%+v", ready, again)
	}
}

func TestPB23ReviewPreservesUnchangedCandidatesAndMarksChangedOnesStale(t *testing.T) {
	fixture := newWorkflowFixture(t)
	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "accept", ID: fixture.requirement,
	}); err != nil {
		t.Fatal(err)
	}
	proposal := readFixtureProposal(t, fixture)
	proposal.Requirements = append(proposal.Requirements, Requirement{
		ID:             "R-02",
		Statement:      "Retries require an explicit action.",
		Classification: "inferred",
		SourceBlocks:   append([]string(nil), proposal.Questions[0].SourceBlocks...),
	})
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)

	grown, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if grown.Summary.Accepted != 1 || grown.Summary.Undecided != 1 || grown.Summary.Stale != 0 {
		t.Fatalf("grown status = %+v", grown)
	}
	if grown.Blockers[0] != (Blocker{Type: "requirement-decision", ID: "R-02"}) {
		t.Fatalf("grown blockers = %+v", grown.Blockers)
	}

	proposal.Requirements[0].Statement = "Changed reviewable statement."
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
	changed, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Summary.Accepted != 0 || changed.Summary.Undecided != 2 || changed.Summary.Stale != 1 {
		t.Fatalf("changed status = %+v", changed)
	}
	if changed.Blockers[0] != (Blocker{Type: "requirement-review-stale", ID: fixture.requirement}) {
		t.Fatalf("changed blockers = %+v", changed.Blockers)
	}
}

func TestPB23ReviewTreatsReorderedSourceProvenanceAsChangedCandidateContent(t *testing.T) {
	fixture := newWorkflowFixture(t)
	proposal := readFixtureProposal(t, fixture)
	proposal.Requirements[0].SourceBlocks = []string{
		proposal.Requirements[0].SourceBlocks[0],
		proposal.Questions[0].SourceBlocks[0],
	}
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
	if _, err := ApplyReview(fixture.root, fixture.proposalPath, fixture.reviewPath, ReviewAction{
		Kind: "accept", ID: fixture.requirement,
	}); err != nil {
		t.Fatal(err)
	}

	proposal.Requirements[0].SourceBlocks[0], proposal.Requirements[0].SourceBlocks[1] =
		proposal.Requirements[0].SourceBlocks[1], proposal.Requirements[0].SourceBlocks[0]
	writeFixtureJSON(t, filepath.Join(fixture.root, fixture.proposalPath), proposal)
	status, err := Status(fixture.root, fixture.proposalPath, fixture.reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.Summary.Accepted != 0 || status.Summary.Stale != 1 {
		t.Fatalf("reordered provenance status = %+v", status)
	}
	if status.Blockers[0] != (Blocker{Type: "requirement-review-stale", ID: fixture.requirement}) {
		t.Fatalf("reordered provenance blockers = %+v", status.Blockers)
	}
}
