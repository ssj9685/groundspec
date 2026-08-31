package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ssj9685/groundspec/internal/intake"
	"github.com/ssj9685/groundspec/internal/pipeline"
	"github.com/ssj9685/groundspec/internal/workflow"
)

type fixtureExpectation struct {
	FrameDigest string `json:"frameDigest"`
	ExitCode    int    `json:"exitCode"`
	Overall     string `json:"overall"`
	Nodes       []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Digest string `json:"digest"`
	} `json:"nodes"`
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestTV09FrameDigestIsLanguageNeutral(t *testing.T) {
	digest := DigestFrames([]Frame{
		{Label: "domain", Value: []byte("groundspec/test/v1")},
		{Label: "empty", Value: []byte{}},
		{Label: "unicode", Value: []byte("한🙂")},
	})
	const want = "cb635b95f0a9a18ac1b469cf34c92ae6e9cc8271afbc2863f29563b9b516cf4d"
	if digest != want {
		t.Fatalf("digest = %s, want %s", digest, want)
	}
}

func TestTV10GoMatchesSharedConformanceFixture(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	fixtureRoot := filepath.Join(root, "testdata", "conformance", "v1")

	expectedBytes, err := os.ReadFile(filepath.Join(fixtureRoot, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected fixtureExpectation
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatal(err)
	}

	graph, err := LoadGraph(fixtureRoot, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall != expected.Overall {
		t.Fatalf("overall = %s, want %s", report.Overall, expected.Overall)
	}
	if len(report.Nodes) != len(expected.Nodes) {
		t.Fatalf("node count = %d, want %d", len(report.Nodes), len(expected.Nodes))
	}
	for index, want := range expected.Nodes {
		got := report.Nodes[index]
		if got.ID != want.ID || got.Status != want.Status || got.Digest != want.Digest {
			t.Fatalf("node %d = %+v, want %+v", index, got, want)
		}
	}
	var output bytes.Buffer
	exitCode, err := Execute([]string{"check", "--graph", "graph.json", "--json"}, fixtureRoot, &output)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != expected.ExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, expected.ExitCode)
	}
}

func TestTV01ProofBecomesFreshThenStale(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "graph.json", `{
  "version": 1,
  "nodes": [
    {"id":"SC-01","inputs":["SPEC.md"]},
    {"id":"T-01","inputs":["test.txt"],"dependsOn":["SC-01"],"proof":".proofs/T-01.json"}
  ]
}`)
	writeTestFile(t, root, "SPEC.md", "v1\n")
	writeTestFile(t, root, "test.txt", "passed\n")

	graph, err := LoadGraph(root, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	before, err := EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if before.Overall != "incomplete" || before.Nodes[1].Status != "missing" {
		t.Fatalf("before attestation = %+v", before)
	}
	if _, err := AttestNode(graph, "T-01", AttestOptions{Result: "passed"}); err != nil {
		t.Fatal(err)
	}
	after, err := EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if after.Overall != "healthy" || after.Nodes[1].Status != "passed" {
		t.Fatalf("after attestation = %+v", after)
	}
	writeTestFile(t, root, "SPEC.md", "v2\n")
	stale, err := EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Overall != "incomplete" || stale.Nodes[1].Status != "stale" {
		t.Fatalf("after input change = %+v", stale)
	}
}

func TestTV05NullOptionalStringsAreRejected(t *testing.T) {
	for _, graphSource := range []string{
		`{"version":1,"nodes":[{"id":"A","kind":null,"inputs":[]}]}`,
		`{"version":1,"nodes":[{"id":"A","inputs":[],"proof":null}]}`,
	} {
		root := t.TempDir()
		writeTestFile(t, root, "graph.json", graphSource)
		if _, err := LoadGraph(root, "graph.json"); err == nil {
			t.Fatalf("graph should be rejected: %s", graphSource)
		}
	}
}

func TestTV09NonUTF8RepositoryEntryNamesAreRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filenames are Unicode")
	}
	root := t.TempDir()
	writeTestFile(t, root, "graph.json", `{"version":1,"nodes":[{"id":"A","inputs":["suite"]}]}`)
	if err := os.Mkdir(filepath.Join(root, "suite"), 0o755); err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(root, "suite") + string(filepath.Separator) + string([]byte{0xff})
	if err := os.WriteFile(invalidPath, []byte("invalid name\n"), 0o644); err != nil {
		t.Skip("filesystem rejects non-UTF-8 names before the checker")
	}
	graph, err := LoadGraph(root, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateGraph(graph); err == nil {
		t.Fatal("non-UTF-8 entry should be rejected")
	}
}

func TestTV04FailedAttestationFailsGraph(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "graph.json", `{"version":1,"nodes":[{"id":"T-01","inputs":["test.txt"],"proof":".proofs/T-01.json"}]}`)
	writeTestFile(t, root, "test.txt", "failed\n")
	graph, err := LoadGraph(root, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttestNode(graph, "T-01", AttestOptions{Result: "failed"}); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall != "failed" || report.Nodes[0].Status != "failed" {
		t.Fatalf("report = %+v", report)
	}
}

func TestTV05MissingDependenciesAndCyclesAreRejected(t *testing.T) {
	graphs := []string{
		`{"version":1,"nodes":[{"id":"A","inputs":[],"dependsOn":["UNKNOWN"]}]}`,
		`{"version":1,"nodes":[{"id":"A","inputs":[],"dependsOn":["B"]},{"id":"B","inputs":[],"dependsOn":["A"]}]}`,
	}
	for _, graphSource := range graphs {
		root := t.TempDir()
		writeTestFile(t, root, "graph.json", graphSource)
		if _, err := LoadGraph(root, "graph.json"); err == nil {
			t.Fatalf("graph should be rejected: %s", graphSource)
		}
	}
}

func TestTV06PathsOutsideTheProjectRootAreRejected(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "graph.json", `{"version":1,"nodes":[{"id":"A","inputs":["../outside.txt"]}]}`)
	if _, err := LoadGraph(root, "graph.json"); err == nil {
		t.Fatal("outside input path should be rejected")
	}

	writeTestFile(t, root, "graph.json", `{"version":1,"nodes":[{"id":"T-01","inputs":["test.txt"],"proof":".proofs/T-01.json"}]}`)
	writeTestFile(t, root, "test.txt", "passed\n")
	graph, err := LoadGraph(root, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttestNode(graph, "T-01", AttestOptions{Result: "passed", Evidence: stringPointer("../outside.txt")}); err == nil {
		t.Fatal("outside evidence path should be rejected")
	}
}

func TestTV07ChangedAndMissingEvidenceMakesProofStale(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "graph.json", `{"version":1,"nodes":[{"id":"T-01","inputs":["test.txt"],"proof":".proofs/T-01.json"}]}`)
	writeTestFile(t, root, "test.txt", "passed\n")
	writeTestFile(t, root, "results/test.txt", "v1\n")
	graph, err := LoadGraph(root, "graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttestNode(graph, "T-01", AttestOptions{Result: "passed", Evidence: stringPointer("results/test.txt")}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "results/test.txt", "v2\n")
	report, err := EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if report.Nodes[0].Status != "stale" || report.Nodes[0].Reason != "evidence-changed" {
		t.Fatalf("changed evidence report = %+v", report)
	}
	if err := os.Remove(filepath.Join(root, "results", "test.txt")); err != nil {
		t.Fatal(err)
	}
	report, err = EvaluateGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if report.Nodes[0].Status != "stale" || report.Nodes[0].Reason != "evidence-missing" {
		t.Fatalf("missing evidence report = %+v", report)
	}
}

func TestTV08CLIExitCodes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "graph.json", `{"version":1,"nodes":[{"id":"T-01","inputs":["test.txt"],"proof":".proofs/T-01.json"}]}`)
	writeTestFile(t, root, "test.txt", "passed\n")
	var output bytes.Buffer
	code, err := Execute([]string{"check", "--graph", "graph.json", "--json"}, root, &output)
	if err != nil || code != 1 {
		t.Fatalf("incomplete check: code=%d err=%v", code, err)
	}
	output.Reset()
	code, err = Execute([]string{"attest", "T-01", "--graph", "graph.json", "--result", "passed", "--evidence", ""}, root, &output)
	if code != 2 || err == nil || !IsExpectedError(err) {
		t.Fatalf("empty evidence: code=%d err=%v", code, err)
	}
	output.Reset()
	code, err = Execute([]string{"attest", "T-01", "--graph", "graph.json", "--result", "passed"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("attest: code=%d err=%v", code, err)
	}
	output.Reset()
	code, err = Execute([]string{"check", "--graph", "graph.json", "--json"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("healthy check: code=%d err=%v", code, err)
	}

	invalidRoot := t.TempDir()
	writeTestFile(t, invalidRoot, "graph.json", `{"version":1,"nodes":[]}`)
	code, err = Execute([]string{"check", "--graph", "graph.json"}, invalidRoot, &output)
	if code != 2 || err == nil || !IsExpectedError(err) {
		t.Fatalf("invalid check: code=%d err=%v", code, err)
	}
}

func TestPB30HelpAndVersionAreInstallSmokeInterfaces(t *testing.T) {
	for _, test := range []struct {
		arguments []string
		contains  string
	}{
		{arguments: []string{"--help"}, contains: "usage: groundspec"},
		{arguments: []string{"--version"}, contains: "groundspec dev"},
	} {
		var output bytes.Buffer
		code, err := Execute(test.arguments, t.TempDir(), &output)
		if err != nil || code != 0 || !strings.Contains(output.String(), test.contains) {
			t.Fatalf("Execute(%v) = code %d, output %q, err %v", test.arguments, code, output.String(), err)
		}
	}
}

func TestTV15IngestCLI(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "brief.md", "# Brief\n\n- Works offline.\n")
	var output bytes.Buffer
	code, err := Execute([]string{"ingest", "brief.md", "--json"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("ingest: code=%d err=%v", code, err)
	}
	var bundle struct {
		Version int `json:"version"`
		Source  struct {
			Path   string `json:"path"`
			Format string `json:"format"`
		} `json:"source"`
		Blocks []struct {
			Text string `json:"text"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(output.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Version != 1 || bundle.Source.Path != "brief.md" || bundle.Source.Format != "markdown" || len(bundle.Blocks) != 2 {
		t.Fatalf("bundle = %+v", bundle)
	}
	output.Reset()
	code, err = Execute([]string{"ingest", "../outside.md", "--json"}, root, &output)
	if code != 2 || err == nil || !IsExpectedError(err) {
		t.Fatalf("outside ingest: code=%d err=%v", code, err)
	}
}

func TestTV20TV21WorkflowCLIExitCodes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "brief.md", "# Brief\n\n- Works offline.\n- How should retries work?\n")
	bundle, err := intake.Ingest(root, "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	bundleBytes = append(bundleBytes, '\n')
	writeTestFile(t, root, ".groundspec/sources/brief.json", string(bundleBytes))
	bundleDigest := sha256.Sum256(bundleBytes)
	proposal := workflow.Proposal{
		Version: 1,
		SourceBundle: workflow.ArtifactReference{
			Path:   ".groundspec/sources/brief.json",
			Digest: hex.EncodeToString(bundleDigest[:]),
		},
		Producer: workflow.Producer{Name: "cli-fixture", Version: "1"},
		Requirements: []workflow.Requirement{
			{ID: "R-01", Statement: "Works offline.", Classification: "explicit", SourceBlocks: []string{bundle.Blocks[1].ID}},
		},
		Questions: []workflow.Question{
			{ID: "Q-01", Question: "How should retries work?", SourceBlocks: []string{bundle.Blocks[2].ID}},
		},
	}
	proposalBytes, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, ".groundspec/proposal.json", string(append(proposalBytes, '\n')))

	var output bytes.Buffer
	code, err := Execute([]string{"proposal", "validate", ".groundspec/proposal.json", "--json"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("proposal validate: code=%d err=%v output=%s", code, err, output.String())
	}
	output.Reset()
	code, err = Execute([]string{"status", ".groundspec/proposal.json", "--json"}, root, &output)
	if err != nil || code != 1 {
		t.Fatalf("blocked status: code=%d err=%v output=%s", code, err, output.String())
	}
	var blocked workflow.StatusReport
	if err := json.Unmarshal(output.Bytes(), &blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.State != "blocked" || len(blocked.Blockers) != 2 {
		t.Fatalf("blocked = %+v", blocked)
	}

	output.Reset()
	code, err = Execute([]string{"review", ".groundspec/proposal.json", "--accept", "R-01", "--note", "Confirmed", "--json"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("accept review: code=%d err=%v output=%s", code, err, output.String())
	}
	output.Reset()
	code, err = Execute([]string{"review", ".groundspec/proposal.json", "--resolve", "Q-01", "--answer", "Retry manually", "--json"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("resolve review: code=%d err=%v output=%s", code, err, output.String())
	}
	output.Reset()
	code, err = Execute([]string{"status", ".groundspec/proposal.json", "--json"}, root, &output)
	if err != nil || code != 0 {
		t.Fatalf("ready status: code=%d err=%v output=%s", code, err, output.String())
	}
	var ready workflow.StatusReport
	if err := json.Unmarshal(output.Bytes(), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.State != "ready" || ready.Next != "materialize" {
		t.Fatalf("ready = %+v", ready)
	}

	output.Reset()
	code, err = Execute([]string{"review", ".groundspec/proposal.json", "--accept", "R-01", "--reject", "R-01"}, root, &output)
	if code != 2 || err == nil || !IsExpectedError(err) {
		t.Fatalf("ambiguous review: code=%d err=%v", code, err)
	}
}

func TestPB21TerminalCompletionRequiresCurrentGraphEvidence(t *testing.T) {
	root := t.TempDir()
	complete := pipeline.LifecycleReport{Version: 1, State: "complete", Next: "none", Blockers: []pipeline.LifecycleBlocker{}}

	missing := requireTerminalGraphEvidence(root, ".groundspec/graph.json", complete)
	if missing.State != "blocked" || missing.Next != "evidence" || len(missing.Blockers) != 1 {
		t.Fatalf("missing graph evidence = %+v", missing)
	}

	writeTestFile(t, root, "source.txt", "source\n")
	writeTestFile(t, root, "verification.txt", "passed\n")
	writeTestFile(t, root, ".groundspec/graph.json", `{
  "version": 1,
  "nodes": [
    {"id":"TERMINAL","kind":"terminal-lifecycle","inputs":["verification.txt"]}
  ]
}`)
	unproved := requireTerminalGraphEvidence(root, ".groundspec/graph.json", complete)
	if unproved.State != "blocked" || len(unproved.Blockers) != 1 || !strings.Contains(unproved.Blockers[0].Reason, "declared") {
		t.Fatalf("unproved terminal graph node = %+v", unproved)
	}

	writeTestFile(t, root, ".groundspec/graph.json", `{
  "version": 1,
  "nodes": [
    {"id":"SOURCE","kind":"source","inputs":["source.txt"],"proof":".groundspec/proofs/source.json"},
    {"id":"TERMINAL","kind":"terminal-lifecycle","inputs":["verification.txt"],"dependsOn":["SOURCE"],"proof":".groundspec/proofs/terminal.json"}
  ]
}`)
	graph, err := LoadGraph(root, ".groundspec/graph.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttestNode(graph, "SOURCE", AttestOptions{Result: "passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := AttestNode(graph, "TERMINAL", AttestOptions{Result: "passed"}); err != nil {
		t.Fatal(err)
	}

	current := requireTerminalGraphEvidence(root, ".groundspec/graph.json", complete)
	if current.State != "complete" || len(current.Blockers) != 0 {
		t.Fatalf("current graph evidence = %+v", current)
	}

	writeTestFile(t, root, "source.txt", "changed\n")
	stale := requireTerminalGraphEvidence(root, ".groundspec/graph.json", complete)
	if stale.State != "blocked" || stale.Next != "evidence" || len(stale.Blockers) != 2 {
		t.Fatalf("stale graph evidence = %+v", stale)
	}
	if !strings.Contains(stale.Blockers[0].Reason, "SOURCE") || !strings.Contains(stale.Blockers[1].Reason, "TERMINAL") {
		t.Fatalf("stale graph blockers do not identify nodes: %+v", stale.Blockers)
	}
}

func TestPB21LifecycleKeepsCompatibilityUntilARepositoryDeclaresAGraph(t *testing.T) {
	root := t.TempDir()
	if path, required := declaredGraphPath(root, ""); required || path != ".groundspec/graph.json" {
		t.Fatalf("undeclared default graph became mandatory: path=%s required=%t", path, required)
	}
	if path, required := declaredGraphPath(root, "custom-graph.json"); !required || path != "custom-graph.json" {
		t.Fatalf("explicit graph was not mandatory: path=%s required=%t", path, required)
	}
	writeTestFile(t, root, ".groundspec/graph.json", `{"version":1,"nodes":[]}`)
	if _, required := declaredGraphPath(root, ""); !required {
		t.Fatal("declared repository graph was ignored")
	}
}
