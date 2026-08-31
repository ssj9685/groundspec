package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ssj9685/groundspec/internal/agent"
	"github.com/ssj9685/groundspec/internal/intake"
	"github.com/ssj9685/groundspec/internal/workflow"
)

const protocolVersion = 1

var protocolIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Reference struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type PlanRequirement struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Behavior           string   `json:"behavior"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
}

type DesignDecision struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	RequirementIDs []string `json:"requirementIds"`
	Choice         string   `json:"choice"`
	Rationale      string   `json:"rationale"`
}

type TestCase struct {
	ID             string   `json:"id"`
	RequirementIDs []string `json:"requirementIds"`
	Level          string   `json:"level"`
	Given          string   `json:"given"`
	When           string   `json:"when"`
	Then           string   `json:"then"`
}

type Task struct {
	ID             string   `json:"id"`
	RequirementIDs []string `json:"requirementIds"`
	Description    string   `json:"description"`
}

type VerificationCommand struct {
	ID               string   `json:"id"`
	WorkingDirectory string   `json:"workingDirectory"`
	Argv             []string `json:"argv"`
	Optional         bool     `json:"optional"`
}

type ImplementationPlan struct {
	Language             string                `json:"language"`
	Framework            string                `json:"framework"`
	SourceDirectory      string                `json:"sourceDirectory"`
	Tasks                []Task                `json:"tasks"`
	VerificationCommands []VerificationCommand `json:"verificationCommands"`
}

type DevelopmentPlan struct {
	Version        int                `json:"version"`
	Adapter        agent.Descriptor   `json:"adapter"`
	Proposal       Reference          `json:"proposal"`
	Review         Reference          `json:"review"`
	ProjectContext *Reference         `json:"projectContext,omitempty"`
	Project        string             `json:"project"`
	Summary        string             `json:"summary"`
	Requirements   []PlanRequirement  `json:"requirements"`
	Design         []DesignDecision   `json:"design"`
	Tests          []TestCase         `json:"tests"`
	Implementation ImplementationPlan `json:"implementation"`
}

type ProjectContext struct {
	Version         int      `json:"version"`
	Project         string   `json:"project"`
	Language        string   `json:"language"`
	Framework       string   `json:"framework"`
	SourceDirectory string   `json:"sourceDirectory"`
	Constraints     []string `json:"constraints"`
}

type draftOutput struct {
	Version      int                    `json:"version"`
	Requirements []workflow.Requirement `json:"requirements"`
	Questions    []workflow.Question    `json:"questions"`
}

type planOutput struct {
	Version        int                `json:"version"`
	Project        string             `json:"project"`
	Summary        string             `json:"summary"`
	Requirements   []PlanRequirement  `json:"requirements"`
	Design         []DesignDecision   `json:"design"`
	Tests          []TestCase         `json:"tests"`
	Implementation ImplementationPlan `json:"implementation"`
}

type Artifact struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type ArtifactChange struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	BeforeDigest string `json:"beforeDigest,omitempty"`
	AfterDigest  string `json:"afterDigest,omitempty"`
}

type ReportedCheck struct {
	Command string `json:"command"`
	Result  string `json:"result"`
}

type implementationOutput struct {
	Summary string          `json:"summary"`
	Files   []string        `json:"files"`
	Checks  []ReportedCheck `json:"checks"`
}

type ImplementationResult struct {
	Version    int              `json:"version"`
	Plan       Reference        `json:"plan"`
	Adapter    agent.Descriptor `json:"adapter"`
	Summary    string           `json:"summary"`
	Artifacts  []Artifact       `json:"artifacts"`
	Changes    []ArtifactChange `json:"changes"`
	Checks     []ReportedCheck  `json:"reportedChecks"`
	RecordedAt string           `json:"recordedAt"`
}

type CommandResult struct {
	ID               string   `json:"id"`
	Argv             []string `json:"argv"`
	WorkingDirectory string   `json:"workingDirectory"`
	ExitCode         int      `json:"exitCode"`
	Passed           bool     `json:"passed"`
	Optional         bool     `json:"optional"`
	Log              Artifact `json:"log"`
}

type VerificationResult struct {
	Version        int             `json:"version"`
	Plan           Reference       `json:"plan"`
	Implementation Reference       `json:"implementation"`
	Passed         bool            `json:"passed"`
	Commands       []CommandResult `json:"commands"`
	RecordedAt     string          `json:"recordedAt"`
}

type LifecycleBlocker struct {
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

type LifecycleReport struct {
	Version  int                `json:"version"`
	State    string             `json:"state"`
	Next     string             `json:"next"`
	Blockers []LifecycleBlocker `json:"blockers"`
}

type Workspace struct {
	Root     string `json:"root"`
	Source   string `json:"source"`
	Bundle   string `json:"bundle"`
	Proposal string `json:"proposal"`
	Review   string `json:"review"`
	Adapter  string `json:"adapter"`
}

var draftSchema = []byte(`{
  "type":"object","additionalProperties":false,
  "required":["version","requirements","questions"],
  "properties":{
    "version":{"type":"integer","const":1},
    "requirements":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","statement","classification","sourceBlocks"],"properties":{"id":{"type":"string"},"statement":{"type":"string"},"classification":{"enum":["explicit","inferred","assumption"]},"sourceBlocks":{"type":"array","items":{"type":"string"},"minItems":1}}}},
    "questions":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","question","sourceBlocks"],"properties":{"id":{"type":"string"},"question":{"type":"string"},"sourceBlocks":{"type":"array","items":{"type":"string"},"minItems":1}}}}
  }
}`)

var planSchema = []byte(`{
  "type":"object","additionalProperties":false,"required":["version","project","summary","requirements","design","tests","implementation"],
  "properties":{
    "version":{"type":"integer","const":1},"project":{"type":"string"},"summary":{"type":"string"},
    "requirements":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","title","behavior","acceptanceCriteria"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"behavior":{"type":"string"},"acceptanceCriteria":{"type":"array","items":{"type":"string"},"minItems":1}}}},
    "design":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","title","requirementIds","choice","rationale"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"requirementIds":{"type":"array","items":{"type":"string"},"minItems":1},"choice":{"type":"string"},"rationale":{"type":"string"}}}},
    "tests":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","requirementIds","level","given","when","then"],"properties":{"id":{"type":"string"},"requirementIds":{"type":"array","items":{"type":"string"},"minItems":1},"level":{"enum":["unit","contract","integration","browser","smoke"]},"given":{"type":"string"},"when":{"type":"string"},"then":{"type":"string"}}}},
    "implementation":{"type":"object","additionalProperties":false,"required":["language","framework","sourceDirectory","tasks","verificationCommands"],"properties":{"language":{"type":"string"},"framework":{"type":"string"},"sourceDirectory":{"type":"string"},"tasks":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["id","requirementIds","description"],"properties":{"id":{"type":"string"},"requirementIds":{"type":"array","items":{"type":"string"},"minItems":1},"description":{"type":"string"}}}},"verificationCommands":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["id","workingDirectory","argv","optional"],"properties":{"id":{"type":"string"},"workingDirectory":{"type":"string"},"argv":{"type":"array","items":{"type":"string"},"minItems":1},"optional":{"type":"boolean"}}}}}}
  }
}`)

var implementationSchema = []byte(`{
  "type":"object","additionalProperties":false,"required":["summary","files","checks"],
  "properties":{"summary":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"checks":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["command","result"],"properties":{"command":{"type":"string"},"result":{"type":"string"}}}}}
}`)

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSONAtomic(root, relative string, value any) error {
	absolute, err := inside(root, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(absolute), ".groundspec-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, absolute)
}

func writeAtomic(root, relative string, data []byte) error {
	absolute, err := inside(root, relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(absolute), ".groundspec-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, absolute)
}

func inside(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path must be project-relative: %s", relative)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if _, err := filepath.EvalSymlinks(absoluteRoot); err != nil {
		return "", fmt.Errorf("project root does not exist: %s", root)
	}
	absolute := filepath.Join(absoluteRoot, filepath.FromSlash(relative))
	relation, err := filepath.Rel(absoluteRoot, absolute)
	if err != nil || relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) || filepath.IsAbs(relation) {
		return "", fmt.Errorf("path resolves outside project: %s", relative)
	}
	current := absoluteRoot
	for _, segment := range strings.Split(relation, string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path traverses a symbolic link: %s", relative)
		}
	}
	return absolute, nil
}

func readInside(root, relative string) ([]byte, error) {
	absolute, err := inside(root, relative)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file: %s", relative)
	}
	return os.ReadFile(absolute)
}

func referencesCurrent(blocks map[string]bool, references []string) bool {
	if len(references) == 0 {
		return false
	}
	for _, reference := range references {
		if !blocks[reference] {
			return false
		}
	}
	return true
}

func preservableDraft(root, proposalPath string, bundle intake.Bundle) draftOutput {
	previousBytes, err := readInside(root, proposalPath)
	if err != nil {
		return draftOutput{Version: protocolVersion, Requirements: []workflow.Requirement{}, Questions: []workflow.Question{}}
	}
	var previous workflow.Proposal
	if decodeStrictJSON(previousBytes, &previous) != nil {
		return draftOutput{Version: protocolVersion, Requirements: []workflow.Requirement{}, Questions: []workflow.Question{}}
	}
	blocks := map[string]bool{}
	for _, block := range bundle.Blocks {
		blocks[block.ID] = true
	}
	preserved := draftOutput{Version: protocolVersion, Requirements: []workflow.Requirement{}, Questions: []workflow.Question{}}
	for _, requirement := range previous.Requirements {
		if referencesCurrent(blocks, requirement.SourceBlocks) {
			preserved.Requirements = append(preserved.Requirements, requirement)
		}
	}
	for _, question := range previous.Questions {
		if referencesCurrent(blocks, question.SourceBlocks) {
			preserved.Questions = append(preserved.Questions, question)
		}
	}
	return preserved
}

func validatePreservedDraft(generated, preserved draftOutput) error {
	requirements := map[string]workflow.Requirement{}
	for _, requirement := range generated.Requirements {
		requirements[requirement.ID] = requirement
	}
	for _, expected := range preserved.Requirements {
		if actual, exists := requirements[expected.ID]; !exists || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("draft changed or removed current requirement %s", expected.ID)
		}
	}
	questions := map[string]workflow.Question{}
	for _, question := range generated.Questions {
		questions[question.ID] = question
	}
	for _, expected := range preserved.Questions {
		if actual, exists := questions[expected.ID]; !exists || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("draft changed or removed current question %s", expected.ID)
		}
	}
	return nil
}

func Draft(ctx context.Context, root, bundlePath, proposalPath string, adapter agent.Adapter) (workflow.Proposal, error) {
	bundleBytes, err := readInside(root, bundlePath)
	if err != nil {
		return workflow.Proposal{}, err
	}
	var bundle intake.Bundle
	if err := decodeStrictJSON(bundleBytes, &bundle); err != nil {
		return workflow.Proposal{}, fmt.Errorf("invalid source bundle: %w", err)
	}
	currentBundle, err := intake.Ingest(root, bundle.Source.Path)
	if err != nil || !reflect.DeepEqual(bundle, currentBundle) {
		return workflow.Proposal{}, errors.New("source bundle is not the current deterministic ingestion result")
	}
	preserved := preservableDraft(root, proposalPath, bundle)
	preservedJSON, _ := json.MarshalIndent(preserved, "", "  ")
	basePrompt := "You are the synthesis adapter for a provider-neutral spec workflow. Read the SourceBundle JSON below. Return only candidates grounded in sourceBlocks. Copy sourceBlocks IDs exactly from the bundle; never invent, abbreviate, or reconstruct an ID. Separate explicit, inferred, and assumption classifications. Put unresolved material decisions in questions. IDs must be unique and stable-looking. Do not approve anything and do not write files. Every candidate in PRESERVED CANDIDATES still points to unchanged source blocks: copy its ID, text, classification, and sourceBlocks byte-for-byte. Do not restate it under another ID. Add only genuinely new candidates from new or changed blocks.\n\nPRESERVED CANDIDATES:\n" + string(preservedJSON) + "\n\nSOURCE BUNDLE:\n" + string(bundleBytes)
	prompt := basePrompt
	var proposal workflow.Proposal
	var validationErr error
	for attempt := 1; attempt <= 2; attempt++ {
		output, runErr := adapter.Run(ctx, agent.Request{Prompt: prompt, Schema: draftSchema, Workdir: root, ReadOnly: true, ReasoningEffort: "medium", Timeout: 5 * time.Minute})
		if runErr != nil {
			return workflow.Proposal{}, runErr
		}
		var drafted draftOutput
		if decodeErr := decodeStrictJSON(output, &drafted); decodeErr != nil {
			validationErr = decodeErr
		} else if preserveErr := validatePreservedDraft(drafted, preserved); preserveErr != nil {
			validationErr = preserveErr
		} else {
			proposal = workflow.Proposal{
				Version:      protocolVersion,
				SourceBundle: workflow.ArtifactReference{Path: bundlePath, Digest: digest(bundleBytes)},
				Producer:     workflow.Producer{Name: adapter.Descriptor().ID, Version: adapter.Descriptor().Version},
				Requirements: drafted.Requirements,
				Questions:    drafted.Questions,
			}
			if writeErr := writeJSONAtomic(root, proposalPath, proposal); writeErr != nil {
				return workflow.Proposal{}, writeErr
			}
			_, validationErr = workflow.ValidateProposal(root, proposalPath)
		}
		if validationErr == nil {
			return proposal, nil
		}
		rejection := struct {
			Version int             `json:"version"`
			Attempt int             `json:"attempt"`
			Error   string          `json:"error"`
			Output  json.RawMessage `json:"output"`
		}{Version: protocolVersion, Attempt: attempt, Error: validationErr.Error(), Output: json.RawMessage(output)}
		if writeErr := writeJSONAtomic(root, ".groundspec/rejections/proposal-latest.json", rejection); writeErr != nil {
			return workflow.Proposal{}, writeErr
		}
		prompt = basePrompt + "\n\nYour previous output was rejected. Return a corrected complete proposal and use only block IDs copied exactly from SOURCE BUNDLE.\nVALIDATION ERROR: " + validationErr.Error() + "\nPREVIOUS OUTPUT:\n" + string(output)
	}
	return workflow.Proposal{}, fmt.Errorf("adapter proposal failed deterministic validation after one correction attempt: %w", validationErr)
}

func Start(ctx context.Context, sourcePath, outputRoot string, adapter agent.Adapter) (Workspace, error) {
	return bootstrap(ctx, sourcePath, outputRoot, adapter, false)
}

// Init adds GroundSpec workflow artifacts to an existing project without
// overwriting project files. Later materialization remains explicit.
func Init(ctx context.Context, sourcePath, outputRoot string, adapter agent.Adapter) (Workspace, error) {
	return bootstrap(ctx, sourcePath, outputRoot, adapter, true)
}

func bootstrap(ctx context.Context, sourcePath, outputRoot string, adapter agent.Adapter, existing bool) (Workspace, error) {
	absoluteOutput, err := filepath.Abs(outputRoot)
	if err != nil {
		return Workspace{}, err
	}
	if entries, readErr := os.ReadDir(absoluteOutput); !existing && readErr == nil && len(entries) > 0 {
		return Workspace{}, errors.New("output workspace must be empty")
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Workspace{}, readErr
	}
	if existing {
		for _, reserved := range []string{".groundspec", "SPEC.md", "TEST_SPEC.md", "test-or-verification/TEST_SPEC.md", "DESIGN.md", "IMPLEMENTATION_PLAN.md"} {
			if _, statErr := os.Lstat(filepath.Join(absoluteOutput, reserved)); statErr == nil {
				return Workspace{}, fmt.Errorf("project already contains reserved GroundSpec path: %s", reserved)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return Workspace{}, statErr
			}
		}
	}
	absoluteSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return Workspace{}, err
	}
	sourceInfo, err := os.Stat(absoluteSource)
	if err != nil || !sourceInfo.Mode().IsRegular() {
		return Workspace{}, fmt.Errorf("source must be a regular file: %s", sourcePath)
	}
	if err := os.MkdirAll(filepath.Join(absoluteOutput, ".groundspec", "source-material"), 0o755); err != nil {
		return Workspace{}, err
	}
	sourceName := filepath.Base(absoluteSource)
	relativeSource := filepath.ToSlash(filepath.Join(".groundspec", "source-material", sourceName))
	raw, err := os.ReadFile(absoluteSource)
	if err != nil {
		return Workspace{}, err
	}
	if err := writeAtomic(absoluteOutput, relativeSource, raw); err != nil {
		return Workspace{}, err
	}
	bundle, err := intake.Ingest(absoluteOutput, relativeSource)
	if err != nil {
		return Workspace{}, err
	}
	bundlePath := ".groundspec/sources/source.json"
	if err := writeJSONAtomic(absoluteOutput, bundlePath, bundle); err != nil {
		return Workspace{}, err
	}
	proposalPath := ".groundspec/proposal.json"
	if _, err := Draft(ctx, absoluteOutput, bundlePath, proposalPath, adapter); err != nil {
		return Workspace{}, err
	}
	reviewPath := ".groundspec/review.json"
	if _, err := workflow.InitializeReview(absoluteOutput, proposalPath, reviewPath); err != nil {
		return Workspace{}, err
	}
	return Workspace{Root: absoluteOutput, Source: relativeSource, Bundle: bundlePath, Proposal: proposalPath, Review: reviewPath, Adapter: adapter.Descriptor().ID}, nil
}

func Materialize(ctx context.Context, root, proposalPath, reviewPath, planPath string, adapter agent.Adapter) (DevelopmentPlan, error) {
	reviewed, err := workflow.LoadReviewedInput(root, proposalPath, reviewPath)
	if err != nil {
		return DevelopmentPlan{}, err
	}
	input, _ := json.MarshalIndent(struct {
		Requirements []workflow.Requirement `json:"acceptedRequirements"`
		Answers      map[string]string      `json:"resolvedQuestions"`
	}{reviewed.Accepted, reviewed.Answers}, "", "  ")
	projectContext, contextReference, err := loadProjectContext(root, ".groundspec/project-context.json")
	if err != nil {
		return DevelopmentPlan{}, err
	}
	contextJSON := []byte("null")
	if projectContext != nil {
		contextJSON, _ = json.MarshalIndent(projectContext, "", "  ")
	}
	inventory := existingInventory(root)
	basePrompt := "Create a minimal but complete development plan from the reviewed input below. Every accepted requirement ID must occur exactly once in requirements and at least once in tests and implementation tasks. Every test, task, and verification command must have a unique non-empty ID and non-empty text fields. Choose argv verification commands, never shell command strings. Set optional=false for completion-gating commands and optional=true only for explicitly optional verification such as an extra browser runtime. Prefer few dependencies and explicit external adapter boundaries. If PROJECT CONTEXT is non-null, its language, framework, sourceDirectory, and constraints are approved invariants: reproduce those three strings exactly and do not propose an architecture replacement. EXISTING FILES are context, not permission to invent a second implementation. Do not write files.\n\nPROJECT CONTEXT:\n" + string(contextJSON) + "\n\nEXISTING FILES:\n" + strings.Join(inventory, "\n") + "\n\nREVIEWED INPUT:\n" + string(input)
	prompt := basePrompt
	var plan DevelopmentPlan
	var validationErr error
	for attempt := 1; attempt <= 2; attempt++ {
		output, runErr := adapter.Run(ctx, agent.Request{Prompt: prompt, Schema: planSchema, Workdir: root, ReadOnly: true, ReasoningEffort: "low", Timeout: 5 * time.Minute})
		if runErr != nil {
			return DevelopmentPlan{}, runErr
		}
		var generated planOutput
		if decodeErr := decodeStrictJSON(output, &generated); decodeErr != nil {
			validationErr = decodeErr
		} else {
			plan = DevelopmentPlan{
				Version: protocolVersion, Adapter: adapter.Descriptor().Public(),
				Proposal:       Reference{Path: reviewed.Proposal.Report.Proposal, Digest: reviewed.Proposal.Report.Digest},
				Review:         Reference{Path: reviewed.ReviewPath, Digest: reviewed.ReviewDigest},
				ProjectContext: contextReference,
				Project:        generated.Project, Summary: generated.Summary, Requirements: generated.Requirements,
				Design: generated.Design, Tests: generated.Tests, Implementation: generated.Implementation,
			}
			validationErr = ValidatePlan(root, plan, reviewed.Accepted)
		}
		if validationErr == nil {
			break
		}
		rejection := struct {
			Version int             `json:"version"`
			Attempt int             `json:"attempt"`
			Error   string          `json:"error"`
			Output  json.RawMessage `json:"output"`
		}{Version: protocolVersion, Attempt: attempt, Error: validationErr.Error(), Output: json.RawMessage(output)}
		if err := writeJSONAtomic(root, ".groundspec/rejections/development-plan-latest.json", rejection); err != nil {
			return DevelopmentPlan{}, err
		}
		prompt = basePrompt + "\n\nYour previous output was rejected by the deterministic validator. Return a corrected complete plan.\nVALIDATION ERROR: " + validationErr.Error() + "\nPREVIOUS OUTPUT:\n" + string(output)
	}
	if validationErr != nil {
		return DevelopmentPlan{}, fmt.Errorf("adapter plan failed deterministic validation after one correction attempt: %w", validationErr)
	}
	if err := writeJSONAtomic(root, planPath, plan); err != nil {
		return DevelopmentPlan{}, err
	}
	for path, contents := range Render(plan) {
		if err := writeAtomic(root, path, []byte(contents)); err != nil {
			return DevelopmentPlan{}, err
		}
	}
	return plan, nil
}

func ValidatePlan(root string, plan DevelopmentPlan, accepted []workflow.Requirement) error {
	if plan.Version != protocolVersion || strings.TrimSpace(plan.Project) == "" || strings.TrimSpace(plan.Summary) == "" {
		return errors.New("plan version, project, and summary are required")
	}
	if err := plan.Adapter.ValidatePublic(); err != nil {
		return fmt.Errorf("plan adapter provenance is invalid: %w", err)
	}
	if strings.TrimSpace(plan.Implementation.Language) == "" || strings.TrimSpace(plan.Implementation.Framework) == "" || strings.TrimSpace(plan.Implementation.SourceDirectory) == "" {
		return errors.New("implementation language, framework, and source directory are required")
	}
	if len(plan.Tests) == 0 || len(plan.Implementation.Tasks) == 0 || len(plan.Implementation.VerificationCommands) == 0 {
		return errors.New("plan requires tests, implementation tasks, and verification commands")
	}
	projectContext, contextReference, err := loadProjectContext(root, ".groundspec/project-context.json")
	if err != nil {
		return err
	}
	if projectContext != nil {
		if plan.ProjectContext == nil || *plan.ProjectContext != *contextReference {
			return errors.New("development plan is not bound to the current project context")
		}
		if plan.Project != projectContext.Project || plan.Implementation.Language != projectContext.Language || plan.Implementation.Framework != projectContext.Framework || plan.Implementation.SourceDirectory != projectContext.SourceDirectory {
			return errors.New("development plan silently replaces approved project language, framework, or source boundary")
		}
	} else if plan.ProjectContext != nil {
		return errors.New("development plan references a project context that does not exist")
	}
	want := map[string]bool{}
	for _, requirement := range accepted {
		want[requirement.ID] = true
	}
	seen := map[string]bool{}
	for _, requirement := range plan.Requirements {
		if !want[requirement.ID] || seen[requirement.ID] {
			return fmt.Errorf("plan contains unknown or duplicate requirement %s", requirement.ID)
		}
		if !protocolIDPattern.MatchString(requirement.ID) || strings.TrimSpace(requirement.Title) == "" || strings.TrimSpace(requirement.Behavior) == "" || len(requirement.AcceptanceCriteria) == 0 {
			return fmt.Errorf("plan requirement %s is incomplete", requirement.ID)
		}
		for _, criterion := range requirement.AcceptanceCriteria {
			if strings.TrimSpace(criterion) == "" {
				return fmt.Errorf("plan requirement %s has an empty acceptance criterion", requirement.ID)
			}
		}
		seen[requirement.ID] = true
	}
	if len(seen) != len(want) {
		return errors.New("plan does not map every accepted requirement")
	}
	tested := map[string]bool{}
	tasked := map[string]bool{}
	ids := map[string]bool{}
	for _, decision := range plan.Design {
		if !protocolIDPattern.MatchString(decision.ID) || ids[decision.ID] {
			return fmt.Errorf("plan contains an invalid or duplicate design id %s", decision.ID)
		}
		if strings.TrimSpace(decision.Title) == "" || strings.TrimSpace(decision.Choice) == "" || strings.TrimSpace(decision.Rationale) == "" || len(decision.RequirementIDs) == 0 {
			return fmt.Errorf("plan design %s is incomplete", decision.ID)
		}
		ids[decision.ID] = true
		mapped := map[string]bool{}
		for _, id := range decision.RequirementIDs {
			if !want[id] || mapped[id] {
				return fmt.Errorf("design %s references an unknown or duplicate requirement %s", decision.ID, id)
			}
			mapped[id] = true
		}
	}
	ids = map[string]bool{}
	for _, test := range plan.Tests {
		if !protocolIDPattern.MatchString(test.ID) {
			return errors.New("plan contains a test with an invalid id")
		}
		if ids[test.ID] {
			return fmt.Errorf("plan contains duplicate test id %s", test.ID)
		}
		if strings.TrimSpace(test.Given) == "" || strings.TrimSpace(test.When) == "" || strings.TrimSpace(test.Then) == "" || len(test.RequirementIDs) == 0 {
			return fmt.Errorf("plan test %s has an empty Given, When, or Then", test.ID)
		}
		switch test.Level {
		case "unit", "contract", "integration", "browser", "smoke":
		default:
			return fmt.Errorf("plan test %s has an invalid level", test.ID)
		}
		ids[test.ID] = true
		mapped := map[string]bool{}
		for _, id := range test.RequirementIDs {
			if !want[id] || mapped[id] {
				return fmt.Errorf("test %s references an unknown or duplicate requirement %s", test.ID, id)
			}
			mapped[id] = true
			tested[id] = true
		}
	}
	if _, err := inside(root, plan.Implementation.SourceDirectory); err != nil {
		return fmt.Errorf("invalid source directory: %w", err)
	}
	ids = map[string]bool{}
	for _, task := range plan.Implementation.Tasks {
		if !protocolIDPattern.MatchString(task.ID) || strings.TrimSpace(task.Description) == "" || len(task.RequirementIDs) == 0 {
			return errors.New("plan contains an implementation task with an empty id or description")
		}
		if ids[task.ID] {
			return fmt.Errorf("plan contains duplicate implementation task id %s", task.ID)
		}
		ids[task.ID] = true
		mapped := map[string]bool{}
		for _, id := range task.RequirementIDs {
			if !want[id] || mapped[id] {
				return fmt.Errorf("task %s references an unknown or duplicate requirement %s", task.ID, id)
			}
			mapped[id] = true
			tasked[id] = true
		}
	}
	ids = map[string]bool{}
	for _, command := range plan.Implementation.VerificationCommands {
		if !protocolIDPattern.MatchString(command.ID) || len(command.Argv) == 0 || strings.TrimSpace(command.Argv[0]) == "" {
			return errors.New("plan contains a verification command with an empty id or argv")
		}
		if ids[command.ID] {
			return fmt.Errorf("plan contains duplicate verification command id %s", command.ID)
		}
		ids[command.ID] = true
		if _, err := inside(root, command.WorkingDirectory); err != nil {
			return fmt.Errorf("verification command %s has invalid working directory", command.ID)
		}
		if err := validateVerificationArgv(command.Argv); err != nil {
			return fmt.Errorf("verification command %s is invalid: %w", command.ID, err)
		}
	}
	for id := range want {
		if !tested[id] || !tasked[id] {
			return fmt.Errorf("requirement %s lacks a test or implementation task", id)
		}
	}
	return nil
}

func validateVerificationArgv(argv []string) error {
	if len(argv) == 0 {
		return errors.New("argv must not be empty")
	}
	executable := argv[0]
	if executable != strings.TrimSpace(executable) || strings.ContainsAny(executable, " \t\r\n") {
		return errors.New("argv[0] must be one executable, not a command string")
	}
	for _, argument := range argv {
		if strings.ContainsRune(argument, '\x00') {
			return errors.New("argv must not contain NUL bytes")
		}
	}
	switch strings.ToLower(filepath.Base(executable)) {
	case "sh", "bash", "dash", "zsh", "ksh", "csh", "tcsh", "fish", "nu", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe", "env", "env.exe":
		return fmt.Errorf("shell-capable executable %q is not allowed", executable)
	}
	return nil
}

func loadProjectContext(root, relative string) (*ProjectContext, *Reference, error) {
	data, err := readInside(root, relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read project context: %w", err)
	}
	var projectContext ProjectContext
	if err := decodeStrictJSON(data, &projectContext); err != nil {
		return nil, nil, fmt.Errorf("project context is invalid: %w", err)
	}
	if projectContext.Version != protocolVersion || strings.TrimSpace(projectContext.Project) == "" || strings.TrimSpace(projectContext.Language) == "" || strings.TrimSpace(projectContext.Framework) == "" || projectContext.Constraints == nil {
		return nil, nil, errors.New("project context version, project, language, and framework are required")
	}
	for _, constraint := range projectContext.Constraints {
		if strings.TrimSpace(constraint) == "" {
			return nil, nil, errors.New("project context constraints must be non-empty strings")
		}
	}
	if _, err := inside(root, projectContext.SourceDirectory); err != nil {
		return nil, nil, fmt.Errorf("project context source directory is invalid: %w", err)
	}
	return &projectContext, &Reference{Path: relative, Digest: digest(data)}, nil
}

func existingInventory(root string) []string {
	paths := []string{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || (strings.HasPrefix(entry.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		relation, err := filepath.Rel(root, path)
		if err == nil && len(paths) < 200 {
			paths = append(paths, filepath.ToSlash(relation))
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}

func Render(plan DevelopmentPlan) map[string]string {
	var spec, design, tests, implementation strings.Builder
	fmt.Fprintf(&spec, "# Spec\n\n%s\n\n## Requirements\n", plan.Summary)
	for _, requirement := range plan.Requirements {
		fmt.Fprintf(&spec, "\n### %s: %s\n\n%s\n\nAcceptance:\n", requirement.ID, requirement.Title, requirement.Behavior)
		for _, criterion := range requirement.AcceptanceCriteria {
			fmt.Fprintf(&spec, "\n- %s", criterion)
		}
		spec.WriteString("\n")
	}
	design.WriteString("# Design\n")
	for _, decision := range plan.Design {
		fmt.Fprintf(&design, "\n## %s: %s\n\nRequirements: %s\n\nChoice: %s\n\nRationale: %s\n", decision.ID, decision.Title, strings.Join(decision.RequirementIDs, ", "), decision.Choice, decision.Rationale)
	}
	tests.WriteString("# Test Spec\n\n| ID | SPEC | Level | Given | When | Then |\n|---|---|---|---|---|---|\n")
	for _, test := range plan.Tests {
		fmt.Fprintf(&tests, "| %s | %s | %s | %s | %s | %s |\n", cell(test.ID), cell(strings.Join(test.RequirementIDs, ", ")), cell(test.Level), cell(test.Given), cell(test.When), cell(test.Then))
	}
	implementation.WriteString("# Implementation Plan\n\n")
	fmt.Fprintf(&implementation, "Language: %s  \nFramework: %s  \nSource directory: `%s`\n\n## Tasks\n", plan.Implementation.Language, plan.Implementation.Framework, plan.Implementation.SourceDirectory)
	for _, task := range plan.Implementation.Tasks {
		fmt.Fprintf(&implementation, "\n- `%s` (%s): %s", task.ID, strings.Join(task.RequirementIDs, ", "), task.Description)
	}
	implementation.WriteString("\n\n## Verification commands\n")
	for _, command := range plan.Implementation.VerificationCommands {
		kind := "required"
		if command.Optional {
			kind = "optional"
		}
		fmt.Fprintf(&implementation, "\n- `%s` (%s) in `%s`: `%s`", command.ID, kind, command.WorkingDirectory, strings.Join(command.Argv, " "))
	}
	implementation.WriteString("\n")
	return map[string]string{"SPEC.md": spec.String(), "DESIGN.md": design.String(), "test-or-verification/TEST_SPEC.md": tests.String(), "IMPLEMENTATION_PLAN.md": implementation.String()}
}

func cell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

func LoadPlan(root, planPath string) (DevelopmentPlan, []byte, error) {
	data, err := readInside(root, planPath)
	if err != nil {
		return DevelopmentPlan{}, nil, err
	}
	var plan DevelopmentPlan
	if err := decodeStrictJSON(data, &plan); err != nil {
		return DevelopmentPlan{}, nil, err
	}
	proposal, err := workflow.LoadReviewedInput(root, plan.Proposal.Path, plan.Review.Path)
	if err != nil {
		return DevelopmentPlan{}, nil, err
	}
	if digestMust(readInside(root, plan.Proposal.Path)) != plan.Proposal.Digest || proposal.ReviewDigest != plan.Review.Digest {
		return DevelopmentPlan{}, nil, errors.New("development plan provenance is stale")
	}
	if err := ValidatePlan(root, plan, proposal.Accepted); err != nil {
		return DevelopmentPlan{}, nil, err
	}
	return plan, data, nil
}

func digestMust(data []byte, err error) string {
	if err != nil {
		return ""
	}
	return digest(data)
}

func Implement(ctx context.Context, root, planPath, resultPath string, adapter agent.Adapter) (ImplementationResult, error) {
	plan, planBytes, err := LoadPlan(root, planPath)
	if err != nil {
		return ImplementationResult{}, err
	}
	before, err := inventory(root, plan.Implementation.SourceDirectory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ImplementationResult{}, err
	}
	planJSON, _ := json.MarshalIndent(plan, "", "  ")
	prompt := "Implement the approved plan in this workspace. The current working directory is already the approved sourceDirectory; write directly in it and do not create another nested sourceDirectory. Do not edit .groundspec, source-material, SPEC.md, DESIGN.md, IMPLEMENTATION_PLAN.md, or test-or-verification/TEST_SPEC.md. Keep the implementation minimal, runnable, and tested. Run useful checks if available. Return the files you touched and checks you ran.\n\nAPPROVED PLAN:\n" + string(planJSON)
	agentWorkdir, err := inside(root, plan.Implementation.SourceDirectory)
	if err != nil {
		return ImplementationResult{}, err
	}
	if err := os.MkdirAll(agentWorkdir, 0o755); err != nil {
		return ImplementationResult{}, err
	}
	output, err := adapter.Run(ctx, agent.Request{Prompt: prompt, Schema: implementationSchema, Workdir: agentWorkdir, AllowExec: true, ReasoningEffort: "high", Timeout: 15 * time.Minute})
	if err != nil {
		return ImplementationResult{}, err
	}
	var reported implementationOutput
	if err := decodeStrictJSON(output, &reported); err != nil {
		return ImplementationResult{}, err
	}
	if strings.TrimSpace(reported.Summary) == "" {
		return ImplementationResult{}, errors.New("implementation result summary must not be blank")
	}
	artifacts, err := inventory(root, plan.Implementation.SourceDirectory)
	if err != nil {
		return ImplementationResult{}, err
	}
	if len(artifacts) == 0 {
		return ImplementationResult{}, errors.New("implementation produced no source artifacts")
	}
	result := ImplementationResult{Version: protocolVersion, Plan: Reference{Path: planPath, Digest: digest(planBytes)}, Adapter: adapter.Descriptor().Public(), Summary: reported.Summary, Artifacts: artifacts, Changes: artifactChanges(before, artifacts), Checks: reported.Checks, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := writeJSONAtomic(root, resultPath, result); err != nil {
		return ImplementationResult{}, err
	}
	return result, nil
}

func inventory(root, relativeDirectory string) ([]Artifact, error) {
	absolute, err := inside(root, relativeDirectory)
	if err != nil {
		return nil, err
	}
	artifacts := []Artifact{}
	err = filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			relation, _ := filepath.Rel(root, path)
			normalized := filepath.ToSlash(relation)
			generated := entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "build" || entry.Name() == "coverage" || entry.Name() == "test-results" || entry.Name() == "playwright-report"
			// Release workflows are implementation artifacts; other hidden trees
			// are repository or agent-private metadata.
			private := path != absolute && strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".github"
			if generated || private || normalized == ".groundspec" || normalized == "source-material" || normalized == "test-or-verification" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relation, _ := filepath.Rel(root, path)
		normalized := filepath.ToSlash(relation)
		lowerPath := strings.ToLower(normalized)
		derivedArchive := strings.HasSuffix(lowerPath, ".zip") || strings.HasSuffix(lowerPath, ".tgz") || strings.HasSuffix(lowerPath, ".tar.gz")
		if derivedArchive || normalized == "SPEC.md" || normalized == "DESIGN.md" || normalized == "IMPLEMENTATION_PLAN.md" || normalized == "groundspec" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, Artifact{Path: normalized, Digest: digest(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(artifacts, func(left, right int) bool { return artifacts[left].Path < artifacts[right].Path })
	return artifacts, nil
}

func artifactChanges(before, after []Artifact) []ArtifactChange {
	left := map[string]string{}
	right := map[string]string{}
	for _, artifact := range before {
		left[artifact.Path] = artifact.Digest
	}
	for _, artifact := range after {
		right[artifact.Path] = artifact.Digest
	}
	paths := map[string]bool{}
	for path := range left {
		paths[path] = true
	}
	for path := range right {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := []ArtifactChange{}
	for _, path := range ordered {
		beforeDigest, hadBefore := left[path]
		afterDigest, hasAfter := right[path]
		if hadBefore && hasAfter && beforeDigest == afterDigest {
			continue
		}
		kind := "modified"
		if !hadBefore {
			kind = "created"
		} else if !hasAfter {
			kind = "removed"
		}
		changes = append(changes, ArtifactChange{Path: path, Kind: kind, BeforeDigest: beforeDigest, AfterDigest: afterDigest})
	}
	return changes
}

func Verify(ctx context.Context, root, planPath, implementationPath, verificationPath string) (VerificationResult, error) {
	plan, planBytes, err := LoadPlan(root, planPath)
	if err != nil {
		return VerificationResult{}, err
	}
	_, implementationBytes, err := loadCurrentImplementation(root, planPath, plan, planBytes, implementationPath)
	if err != nil {
		return VerificationResult{}, err
	}
	result := VerificationResult{Version: protocolVersion, Plan: Reference{Path: planPath, Digest: digest(planBytes)}, Implementation: Reference{Path: implementationPath, Digest: digest(implementationBytes)}, Passed: true, Commands: []CommandResult{}, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, planned := range plan.Implementation.VerificationCommands {
		workingDirectory, err := inside(root, planned.WorkingDirectory)
		if err != nil {
			return VerificationResult{}, err
		}
		command := exec.CommandContext(ctx, planned.Argv[0], planned.Argv[1:]...)
		command.Dir = workingDirectory
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &output
		runErr := command.Run()
		exitCode := 0
		if runErr != nil {
			if !planned.Optional {
				result.Passed = false
			}
			var exitError *exec.ExitError
			if errors.As(runErr, &exitError) {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = -1
				fmt.Fprintf(&output, "\nexecution error: %v\n", runErr)
			}
		}
		logPath := filepath.ToSlash(filepath.Join("test-or-verification", "commands", planned.ID+".log"))
		if err := writeAtomic(root, logPath, output.Bytes()); err != nil {
			return VerificationResult{}, err
		}
		result.Commands = append(result.Commands, CommandResult{ID: planned.ID, Argv: append([]string{}, planned.Argv...), WorkingDirectory: planned.WorkingDirectory, ExitCode: exitCode, Passed: runErr == nil, Optional: planned.Optional, Log: Artifact{Path: logPath, Digest: digest(output.Bytes())}})
	}
	if err := writeJSONAtomic(root, verificationPath, result); err != nil {
		return VerificationResult{}, err
	}
	return result, nil
}

func loadCurrentImplementation(root, planPath string, plan DevelopmentPlan, planBytes []byte, implementationPath string) (ImplementationResult, []byte, error) {
	implementationBytes, err := readInside(root, implementationPath)
	if err != nil {
		return ImplementationResult{}, nil, err
	}
	var implementation ImplementationResult
	if err := decodeStrictJSON(implementationBytes, &implementation); err != nil {
		return ImplementationResult{}, nil, err
	}
	if implementation.Version != protocolVersion || implementation.Plan.Path != planPath || implementation.Plan.Digest != digest(planBytes) {
		return ImplementationResult{}, nil, errors.New("implementation result is invalid or stale")
	}
	if strings.TrimSpace(implementation.Summary) == "" {
		return ImplementationResult{}, nil, errors.New("implementation result summary must not be blank")
	}
	if _, err := time.Parse(time.RFC3339Nano, implementation.RecordedAt); err != nil {
		return ImplementationResult{}, nil, errors.New("implementation result has an invalid recordedAt value")
	}
	if err := implementation.Adapter.ValidatePublic(); err != nil {
		return ImplementationResult{}, nil, fmt.Errorf("implementation adapter provenance is invalid: %w", err)
	}
	if err := artifactsCurrent(root, plan.Implementation.SourceDirectory, implementation.Artifacts); err != nil {
		return ImplementationResult{}, nil, err
	}
	return implementation, implementationBytes, nil
}

func artifactsCurrent(root, sourceDirectory string, artifacts []Artifact) error {
	if len(artifacts) == 0 {
		return errors.New("artifact list is empty")
	}
	current, err := inventory(root, sourceDirectory)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(artifacts, current) {
		return nil
	}
	changes := artifactChanges(artifacts, current)
	if len(changes) == 0 {
		return errors.New("artifact inventory is not canonical")
	}
	return fmt.Errorf("artifact inventory has changed: %s (%s)", changes[0].Path, changes[0].Kind)
}

func validateVerificationRecord(root, planPath string, plan DevelopmentPlan, planBytes []byte, implementationPath string, implementationBytes []byte, verification VerificationResult) error {
	if verification.Version != protocolVersion || verification.Plan.Path != planPath || verification.Plan.Digest != digest(planBytes) {
		return errors.New("verification plan reference is invalid or stale")
	}
	if _, err := time.Parse(time.RFC3339Nano, verification.RecordedAt); err != nil {
		return errors.New("verification result has an invalid recordedAt value")
	}
	if verification.Implementation.Path != implementationPath || verification.Implementation.Digest != digest(implementationBytes) {
		return errors.New("verification implementation reference is invalid or stale")
	}
	if len(verification.Commands) != len(plan.Implementation.VerificationCommands) {
		return errors.New("verification result does not contain every reviewed command")
	}
	allPassed := true
	for index, planned := range plan.Implementation.VerificationCommands {
		command := verification.Commands[index]
		if command.ID != planned.ID || command.WorkingDirectory != planned.WorkingDirectory || command.Optional != planned.Optional || !reflect.DeepEqual(command.Argv, planned.Argv) {
			return fmt.Errorf("verification command %d does not match the reviewed argv", index)
		}
		if command.Passed != (command.ExitCode == 0) {
			return fmt.Errorf("verification command %s has inconsistent pass and exit status", command.ID)
		}
		expectedLog := filepath.ToSlash(filepath.Join("test-or-verification", "commands", planned.ID+".log"))
		if command.Log.Path != expectedLog {
			return fmt.Errorf("verification command %s has an unexpected evidence path", command.ID)
		}
		data, err := readInside(root, command.Log.Path)
		if err != nil || digest(data) != command.Log.Digest {
			return fmt.Errorf("verification log is missing or stale: %s", command.ID)
		}
		allPassed = allPassed && (command.Passed || command.Optional)
	}
	if verification.Passed != allPassed {
		return errors.New("verification aggregate status is inconsistent")
	}
	return nil
}

func Lifecycle(root, proposalPath, reviewPath, planPath, implementationPath, verificationPath string) LifecycleReport {
	report := LifecycleReport{Version: protocolVersion, State: "complete", Next: "none", Blockers: []LifecycleBlocker{}}
	block := func(stage, reason string) {
		report.Blockers = append(report.Blockers, LifecycleBlocker{Stage: stage, Reason: reason})
	}
	status, err := workflow.Status(root, proposalPath, reviewPath)
	if err != nil {
		block("source-proposal", err.Error())
	} else if status.State != "ready" {
		block("review", "review is blocked")
	}
	plan, planBytes, planErr := LoadPlan(root, planPath)
	if planErr != nil {
		block("materialization", planErr.Error())
	}
	var implementationBytes []byte
	var implementationErr error
	if planErr != nil {
		implementationErr = errors.New("development plan is invalid or stale")
	} else {
		_, implementationBytes, implementationErr = loadCurrentImplementation(root, planPath, plan, planBytes, implementationPath)
	}
	if implementationErr != nil {
		block("implementation", implementationErr.Error())
	}
	verificationBytes, verificationErr := readInside(root, verificationPath)
	var verification VerificationResult
	if verificationErr != nil {
		block("verification", "verification result is missing")
	} else {
		if err := decodeStrictJSON(verificationBytes, &verification); err != nil || implementationErr != nil || planErr != nil {
			block("verification", "verification result is invalid or stale")
		} else if err := validateVerificationRecord(root, planPath, plan, planBytes, implementationPath, implementationBytes, verification); err != nil {
			block("verification", err.Error())
		} else if !verification.Passed {
			block("verification", "one or more commands failed")
		}
	}
	_ = plan
	if len(report.Blockers) > 0 {
		report.State = "blocked"
		report.Next = report.Blockers[0].Stage
	}
	return report
}

func Copy(reader io.Reader) ([]byte, error) { return io.ReadAll(reader) }
