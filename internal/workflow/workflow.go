package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/ssj9685/groundspec/internal/intake"
)

const protocolVersion = 1

var blockIDPattern = regexp.MustCompile(`^B-[a-f0-9]{12}$`)

type ArtifactReference struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type Producer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Requirement struct {
	ID             string   `json:"id"`
	Statement      string   `json:"statement"`
	Classification string   `json:"classification"`
	SourceBlocks   []string `json:"sourceBlocks"`
}

type Question struct {
	ID           string   `json:"id"`
	Question     string   `json:"question"`
	SourceBlocks []string `json:"sourceBlocks"`
}

type Proposal struct {
	Version      int               `json:"version"`
	SourceBundle ArtifactReference `json:"sourceBundle"`
	Producer     Producer          `json:"producer"`
	Requirements []Requirement     `json:"requirements"`
	Questions    []Question        `json:"questions"`
}

type ProposalReport struct {
	Version      int      `json:"version"`
	Proposal     string   `json:"proposal"`
	Digest       string   `json:"digest"`
	ReviewDigest string   `json:"reviewDigest"`
	SourceBundle string   `json:"sourceBundle"`
	Producer     Producer `json:"producer"`
	Requirements int      `json:"requirements"`
	Questions    int      `json:"questions"`
}

type ValidatedProposal struct {
	Proposal Proposal
	Bundle   intake.Bundle
	Report   ProposalReport
}

type Decision struct {
	ID              string  `json:"id"`
	CandidateDigest string  `json:"candidateDigest,omitempty"`
	Decision        string  `json:"decision"`
	Note            *string `json:"note,omitempty"`
}

type ReviewReference struct {
	Path               string `json:"path"`
	LegacyReviewDigest string `json:"reviewDigest,omitempty"`
}

type Resolution struct {
	ID              string `json:"id"`
	CandidateDigest string `json:"candidateDigest,omitempty"`
	Answer          string `json:"answer"`
}

type Review struct {
	Version     int             `json:"version"`
	Proposal    ReviewReference `json:"proposal"`
	Decisions   []Decision      `json:"decisions"`
	Resolutions []Resolution    `json:"resolutions"`
}

type ReviewAction struct {
	Kind string
	ID   string
	Text string
}

type StatusSummary struct {
	Requirements int `json:"requirements"`
	Accepted     int `json:"accepted"`
	Rejected     int `json:"rejected"`
	Undecided    int `json:"undecided"`
	Stale        int `json:"stale"`
	Questions    int `json:"questions"`
	Resolved     int `json:"resolved"`
	Unresolved   int `json:"unresolved"`
}

type Blocker struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type StatusReport struct {
	Version  int           `json:"version"`
	Proposal string        `json:"proposal"`
	Review   string        `json:"review"`
	State    string        `json:"state"`
	Next     string        `json:"next"`
	Summary  StatusSummary `json:"summary"`
	Blockers []Blocker     `json:"blockers"`
}

type ReviewedInput struct {
	Proposal     *ValidatedProposal
	ReviewPath   string
	ReviewDigest string
	Accepted     []Requirement
	Answers      map[string]string
}

type optionalString struct {
	Value   string
	Present bool
	Null    bool
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	value.Present = true
	if bytes.Equal(data, []byte("null")) {
		value.Null = true
		return nil
	}
	return json.Unmarshal(data, &value.Value)
}

type reviewDecisionDocument struct {
	ID              string         `json:"id"`
	CandidateDigest string         `json:"candidateDigest"`
	Decision        string         `json:"decision"`
	Note            optionalString `json:"note"`
}

type reviewDocument struct {
	Version     int                      `json:"version"`
	Proposal    ReviewReference          `json:"proposal"`
	Decisions   []reviewDecisionDocument `json:"decisions"`
	Resolutions []Resolution             `json:"resolutions"`
}

func decodeStrict(data []byte, destination any) error {
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

func rawSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type digestFrame struct {
	label string
	value string
}

func digestFrames(frames []digestFrame) string {
	hash := sha256.New()
	var labelLength [4]byte
	var valueLength [8]byte
	for _, frame := range frames {
		label := []byte(frame.label)
		value := []byte(frame.value)
		binary.BigEndian.PutUint32(labelLength[:], uint32(len(label)))
		binary.BigEndian.PutUint64(valueLength[:], uint64(len(value)))
		_, _ = hash.Write(labelLength[:])
		_, _ = hash.Write(label)
		_, _ = hash.Write(valueLength[:])
		_, _ = hash.Write(value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func candidateDigest(kind, id, text, classification string, sourceBlocks []string) string {
	frames := []digestFrame{
		{label: "domain", value: "groundspec/review-candidate/v1"},
		{label: "candidate-kind", value: kind},
		{label: "candidate-id", value: id},
		{label: "candidate-text", value: text},
		{label: "candidate-classification", value: classification},
	}
	for _, blockID := range sourceBlocks {
		frames = append(frames, digestFrame{label: "source-block", value: blockID})
	}
	return digestFrames(frames)
}

func candidateDigestForRequirement(requirement Requirement) string {
	return candidateDigest("requirement", requirement.ID, requirement.Statement, requirement.Classification, requirement.SourceBlocks)
}

func candidateDigestForQuestion(question Question) string {
	return candidateDigest("question", question.ID, question.Question, "", question.SourceBlocks)
}

func reviewSurfaceDigest(proposal Proposal) string {
	type candidate struct {
		id     string
		digest string
	}
	candidates := make([]candidate, 0, len(proposal.Requirements)+len(proposal.Questions))
	for _, requirement := range proposal.Requirements {
		candidates = append(candidates, candidate{
			id:     requirement.ID,
			digest: candidateDigestForRequirement(requirement),
		})
	}
	for _, question := range proposal.Questions {
		candidates = append(candidates, candidate{
			id:     question.ID,
			digest: candidateDigestForQuestion(question),
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		return bytes.Compare([]byte(candidates[left].id), []byte(candidates[right].id)) < 0
	})
	frames := []digestFrame{{label: "domain", value: "groundspec/review-surface/v1"}}
	for _, candidate := range candidates {
		frames = append(frames,
			digestFrame{label: "candidate-id", value: candidate.id},
			digestFrame{label: "candidate-digest", value: candidate.digest},
		)
	}
	return digestFrames(frames)
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func nonBlank(value string) bool {
	return strings.TrimSpace(value) != ""
}

func normalizedRoot(projectRoot string) (string, error) {
	absolute, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("project root does not exist: %s", projectRoot)
	}
	return root, nil
}

func resolveInside(root, relativePath, label string) (string, string, error) {
	if !nonBlank(relativePath) || filepath.IsAbs(relativePath) {
		return "", "", fmt.Errorf("%s must be a non-empty project-relative path", label)
	}
	absolute := filepath.Join(root, filepath.FromSlash(relativePath))
	relation, err := filepath.Rel(root, absolute)
	if err != nil || relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) || filepath.IsAbs(relation) {
		return "", "", fmt.Errorf("%s resolves outside the project root: %s", label, relativePath)
	}
	return absolute, filepath.ToSlash(relation), nil
}

func assertNoSymlinkParents(root, absolutePath, label string) error {
	relation, err := filepath.Rel(root, absolutePath)
	if err != nil {
		return err
	}
	segments := strings.Split(relation, string(filepath.Separator))
	current := root
	for _, segment := range segments[:max(0, len(segments)-1)] {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s traverses a symbolic-link directory", label)
		}
	}
	return nil
}

func readProjectFile(root, relativePath, label string) ([]byte, string, error) {
	absolute, normalized, err := resolveInside(root, relativePath, label)
	if err != nil {
		return nil, "", err
	}
	if err := assertNoSymlinkParents(root, absolute, label); err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("%s does not exist: %s", label, normalized)
	}
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("%s must be a regular file: %s", label, normalized)
	}
	data, err := os.ReadFile(absolute)
	return data, normalized, err
}

func validateBlockReferences(owner string, references []string, blocks map[string]bool) error {
	if len(references) == 0 {
		return fmt.Errorf("%s must reference at least one source block", owner)
	}
	seen := map[string]bool{}
	for _, blockID := range references {
		if !blockIDPattern.MatchString(blockID) {
			return fmt.Errorf("%s has invalid source block id '%s'", owner, blockID)
		}
		if seen[blockID] {
			return fmt.Errorf("%s contains duplicate source block '%s'", owner, blockID)
		}
		seen[blockID] = true
		if !blocks[blockID] {
			return fmt.Errorf("%s references missing source block '%s'", owner, blockID)
		}
	}
	return nil
}

func ValidateProposal(projectRoot, proposalPath string) (*ValidatedProposal, error) {
	root, err := normalizedRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	proposalBytes, normalizedProposalPath, err := readProjectFile(root, proposalPath, "proposal")
	if err != nil {
		return nil, err
	}
	var proposal Proposal
	if err := decodeStrict(proposalBytes, &proposal); err != nil {
		return nil, fmt.Errorf("proposal is not valid JSON: %w", err)
	}
	if proposal.Version != protocolVersion {
		return nil, fmt.Errorf("proposal version must be %d", protocolVersion)
	}
	if !nonBlank(proposal.SourceBundle.Path) || !validDigest(proposal.SourceBundle.Digest) {
		return nil, errors.New("proposal sourceBundle must contain a relative path and lowercase SHA-256 digest")
	}
	if !nonBlank(proposal.Producer.Name) || !nonBlank(proposal.Producer.Version) {
		return nil, errors.New("proposal producer name and version must be non-empty")
	}
	if proposal.Requirements == nil || proposal.Questions == nil {
		return nil, errors.New("proposal requirements and questions must be arrays")
	}

	bundleBytes, normalizedBundlePath, err := readProjectFile(root, proposal.SourceBundle.Path, "source bundle")
	if err != nil {
		return nil, err
	}
	if digest := rawSHA256(bundleBytes); digest != proposal.SourceBundle.Digest {
		return nil, fmt.Errorf("source bundle digest differs: got %s", digest)
	}
	var bundle intake.Bundle
	if err := decodeStrict(bundleBytes, &bundle); err != nil {
		return nil, fmt.Errorf("source bundle is not valid JSON: %w", err)
	}
	currentBundle, err := intake.Ingest(root, bundle.Source.Path)
	if err != nil {
		return nil, fmt.Errorf("cannot reproduce source bundle: %w", err)
	}
	if !reflect.DeepEqual(bundle, currentBundle) {
		return nil, errors.New("source bundle is not the current ingestion result")
	}

	blocks := make(map[string]bool, len(bundle.Blocks))
	for _, block := range bundle.Blocks {
		if blocks[block.ID] {
			return nil, fmt.Errorf("source bundle contains duplicate block id '%s'", block.ID)
		}
		blocks[block.ID] = true
	}
	ids := map[string]bool{}
	for index, requirement := range proposal.Requirements {
		owner := fmt.Sprintf("requirement at index %d", index)
		if !nonBlank(requirement.ID) || !nonBlank(requirement.Statement) {
			return nil, fmt.Errorf("%s must have a non-empty id and statement", owner)
		}
		if ids[requirement.ID] {
			return nil, fmt.Errorf("proposal contains duplicate id '%s'", requirement.ID)
		}
		ids[requirement.ID] = true
		if requirement.Classification != "explicit" && requirement.Classification != "inferred" && requirement.Classification != "assumption" {
			return nil, fmt.Errorf("requirement '%s' has invalid classification", requirement.ID)
		}
		if err := validateBlockReferences("requirement '"+requirement.ID+"'", requirement.SourceBlocks, blocks); err != nil {
			return nil, err
		}
	}
	for index, question := range proposal.Questions {
		owner := fmt.Sprintf("question at index %d", index)
		if !nonBlank(question.ID) || !nonBlank(question.Question) {
			return nil, fmt.Errorf("%s must have a non-empty id and question", owner)
		}
		if ids[question.ID] {
			return nil, fmt.Errorf("proposal contains duplicate id '%s'", question.ID)
		}
		ids[question.ID] = true
		if err := validateBlockReferences("question '"+question.ID+"'", question.SourceBlocks, blocks); err != nil {
			return nil, err
		}
	}

	return &ValidatedProposal{
		Proposal: proposal,
		Bundle:   bundle,
		Report: ProposalReport{
			Version:      protocolVersion,
			Proposal:     normalizedProposalPath,
			Digest:       rawSHA256(proposalBytes),
			ReviewDigest: reviewSurfaceDigest(proposal),
			SourceBundle: normalizedBundlePath,
			Producer:     proposal.Producer,
			Requirements: len(proposal.Requirements),
			Questions:    len(proposal.Questions),
		},
	}, nil
}

func parseReview(data []byte, validated *ValidatedProposal) (*Review, error) {
	var document reviewDocument
	if err := decodeStrict(data, &document); err != nil {
		return nil, fmt.Errorf("review is not valid JSON: %w", err)
	}
	if document.Version != protocolVersion {
		return nil, fmt.Errorf("review version must be %d", protocolVersion)
	}
	if document.Decisions == nil || document.Resolutions == nil {
		return nil, errors.New("review decisions and resolutions must be arrays")
	}
	if document.Proposal.Path != validated.Report.Proposal {
		return nil, errors.New("review is bound to a different proposal path")
	}

	requirementOrder := map[string]int{}
	for index, requirement := range validated.Proposal.Requirements {
		requirementOrder[requirement.ID] = index
	}
	questionOrder := map[string]int{}
	for index, question := range validated.Proposal.Questions {
		questionOrder[question.ID] = index
	}
	review := &Review{
		Version:     document.Version,
		Proposal:    document.Proposal,
		Decisions:   make([]Decision, 0, len(document.Decisions)),
		Resolutions: append([]Resolution{}, document.Resolutions...),
	}
	seen := map[string]bool{}
	for _, candidate := range document.Decisions {
		_, isRequirement := requirementOrder[candidate.ID]
		if !isRequirement {
			return nil, fmt.Errorf("review decision id '%s' is not a requirement", candidate.ID)
		}
		if seen[candidate.ID] {
			return nil, fmt.Errorf("review contains duplicate decision for '%s'", candidate.ID)
		}
		seen[candidate.ID] = true
		if candidate.Decision != "accepted" && candidate.Decision != "rejected" {
			return nil, fmt.Errorf("review decision for '%s' must be accepted or rejected", candidate.ID)
		}
		if candidate.CandidateDigest != "" && !validDigest(candidate.CandidateDigest) {
			return nil, fmt.Errorf("review decision for '%s' has an invalid candidate digest", candidate.ID)
		}
		if candidate.Note.Null {
			return nil, fmt.Errorf("review decision for '%s' note must be a string", candidate.ID)
		}
		var note *string
		if candidate.Note.Present {
			if !nonBlank(candidate.Note.Value) {
				return nil, fmt.Errorf("review decision for '%s' note must be non-empty", candidate.ID)
			}
			value := candidate.Note.Value
			note = &value
		}
		review.Decisions = append(review.Decisions, Decision{ID: candidate.ID, CandidateDigest: candidate.CandidateDigest, Decision: candidate.Decision, Note: note})
	}

	seen = map[string]bool{}
	for _, resolution := range document.Resolutions {
		_, isQuestion := questionOrder[resolution.ID]
		if !isQuestion {
			return nil, fmt.Errorf("review resolution id '%s' is not a question", resolution.ID)
		}
		if seen[resolution.ID] {
			return nil, fmt.Errorf("review contains duplicate resolution for '%s'", resolution.ID)
		}
		seen[resolution.ID] = true
		if !nonBlank(resolution.Answer) {
			return nil, fmt.Errorf("review resolution for '%s' answer must be non-empty", resolution.ID)
		}
		if resolution.CandidateDigest != "" && !validDigest(resolution.CandidateDigest) {
			return nil, fmt.Errorf("review resolution for '%s' has an invalid candidate digest", resolution.ID)
		}
	}
	return review, nil
}

func readReview(root, reviewPath string, validated *ValidatedProposal) (*Review, string, error) {
	absolute, normalized, err := resolveInside(root, reviewPath, "review path")
	if err != nil {
		return nil, "", err
	}
	if err := assertNoSymlinkParents(root, absolute, "review path"); err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(absolute)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, normalized, nil
	}
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("review must be a regular file: %s", normalized)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, "", err
	}
	review, err := parseReview(data, validated)
	return review, normalized, err
}

func writeReview(root, reviewPath string, review Review) error {
	absolute, _, err := resolveInside(root, reviewPath, "review path")
	if err != nil {
		return err
	}
	directory := filepath.Dir(absolute)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if err := assertNoSymlinkParents(root, absolute, "review path"); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(absolute)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(review); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, absolute)
}

// InitializeReview creates the separate, versioned review gate for a proposal.
// It is idempotent so workspace bootstrap can be retried without discarding
// decisions that have already been recorded.
func InitializeReview(projectRoot, proposalPath, reviewPath string) (Review, error) {
	validated, err := ValidateProposal(projectRoot, proposalPath)
	if err != nil {
		return Review{}, err
	}
	root, err := normalizedRoot(projectRoot)
	if err != nil {
		return Review{}, err
	}
	review, normalizedReviewPath, err := readReview(root, reviewPath, validated)
	if err != nil {
		return Review{}, err
	}
	if normalizedReviewPath == validated.Report.Proposal {
		return Review{}, errors.New("review path must differ from proposal path")
	}
	if review != nil {
		return *review, nil
	}
	review = &Review{
		Version:     protocolVersion,
		Proposal:    ReviewReference{Path: validated.Report.Proposal},
		Decisions:   []Decision{},
		Resolutions: []Resolution{},
	}
	if err := writeReview(root, normalizedReviewPath, *review); err != nil {
		return Review{}, err
	}
	return *review, nil
}

func ApplyReview(projectRoot, proposalPath, reviewPath string, action ReviewAction) (Review, error) {
	validated, err := ValidateProposal(projectRoot, proposalPath)
	if err != nil {
		return Review{}, err
	}
	root, err := normalizedRoot(projectRoot)
	if err != nil {
		return Review{}, err
	}
	review, normalizedReviewPath, err := readReview(root, reviewPath, validated)
	if err != nil {
		return Review{}, err
	}
	if normalizedReviewPath == validated.Report.Proposal {
		return Review{}, errors.New("review path must differ from proposal path")
	}
	if review == nil {
		review = &Review{
			Version:     protocolVersion,
			Proposal:    ReviewReference{Path: validated.Report.Proposal},
			Decisions:   []Decision{},
			Resolutions: []Resolution{},
		}
	}
	review.Proposal.LegacyReviewDigest = ""

	requirements := map[string]bool{}
	for _, requirement := range validated.Proposal.Requirements {
		requirements[requirement.ID] = true
	}
	questions := map[string]bool{}
	for _, question := range validated.Proposal.Questions {
		questions[question.ID] = true
	}
	if !nonBlank(action.ID) {
		return Review{}, errors.New("review action requires an id")
	}
	switch action.Kind {
	case "accept", "reject":
		if !requirements[action.ID] {
			return Review{}, fmt.Errorf("review id '%s' is not a requirement", action.ID)
		}
		if action.Text != "" && !nonBlank(action.Text) {
			return Review{}, errors.New("review note must be non-empty when provided")
		}
		decision := "accepted"
		if action.Kind == "reject" {
			decision = "rejected"
		}
		var note *string
		if action.Text != "" {
			value := action.Text
			note = &value
		}
		byID := map[string]Decision{}
		for _, existing := range review.Decisions {
			byID[existing.ID] = existing
		}
		var requirement Requirement
		for _, candidate := range validated.Proposal.Requirements {
			if candidate.ID == action.ID {
				requirement = candidate
				break
			}
		}
		byID[action.ID] = Decision{ID: action.ID, CandidateDigest: candidateDigestForRequirement(requirement), Decision: decision, Note: note}
		review.Decisions = review.Decisions[:0]
		for _, candidate := range byID {
			review.Decisions = append(review.Decisions, candidate)
		}
		sort.Slice(review.Decisions, func(left, right int) bool {
			return bytes.Compare([]byte(review.Decisions[left].ID), []byte(review.Decisions[right].ID)) < 0
		})
	case "resolve":
		if !questions[action.ID] {
			return Review{}, fmt.Errorf("review id '%s' is not a question", action.ID)
		}
		if !nonBlank(action.Text) {
			return Review{}, errors.New("question resolution requires a non-empty answer")
		}
		byID := map[string]Resolution{}
		for _, existing := range review.Resolutions {
			byID[existing.ID] = existing
		}
		var question Question
		for _, candidate := range validated.Proposal.Questions {
			if candidate.ID == action.ID {
				question = candidate
				break
			}
		}
		byID[action.ID] = Resolution{ID: action.ID, CandidateDigest: candidateDigestForQuestion(question), Answer: action.Text}
		review.Resolutions = review.Resolutions[:0]
		for _, candidate := range byID {
			review.Resolutions = append(review.Resolutions, candidate)
		}
		sort.Slice(review.Resolutions, func(left, right int) bool {
			return bytes.Compare([]byte(review.Resolutions[left].ID), []byte(review.Resolutions[right].ID)) < 0
		})
	default:
		return Review{}, fmt.Errorf("unsupported review action '%s'", action.Kind)
	}
	if err := writeReview(root, normalizedReviewPath, *review); err != nil {
		return Review{}, err
	}
	return *review, nil
}

func Status(projectRoot, proposalPath, reviewPath string) (StatusReport, error) {
	validated, err := ValidateProposal(projectRoot, proposalPath)
	if err != nil {
		return StatusReport{}, err
	}
	root, err := normalizedRoot(projectRoot)
	if err != nil {
		return StatusReport{}, err
	}
	review, normalizedReviewPath, err := readReview(root, reviewPath, validated)
	if err != nil {
		return StatusReport{}, err
	}
	decisions := map[string]Decision{}
	resolutions := map[string]Resolution{}
	if review != nil {
		for _, decision := range review.Decisions {
			decisions[decision.ID] = decision
		}
		for _, resolution := range review.Resolutions {
			resolutions[resolution.ID] = resolution
		}
	}
	report := StatusReport{
		Version:  protocolVersion,
		Proposal: validated.Report.Proposal,
		Review:   normalizedReviewPath,
		State:    "ready",
		Next:     "materialize",
		Summary: StatusSummary{
			Requirements: len(validated.Proposal.Requirements),
			Questions:    len(validated.Proposal.Questions),
		},
		Blockers: []Blocker{},
	}
	for _, requirement := range validated.Proposal.Requirements {
		decision, exists := decisions[requirement.ID]
		if exists && decision.CandidateDigest != candidateDigestForRequirement(requirement) {
			report.Summary.Undecided++
			report.Summary.Stale++
			report.Blockers = append(report.Blockers, Blocker{Type: "requirement-review-stale", ID: requirement.ID})
			continue
		}
		switch decision.Decision {
		case "accepted":
			report.Summary.Accepted++
		case "rejected":
			report.Summary.Rejected++
		default:
			report.Summary.Undecided++
			report.Blockers = append(report.Blockers, Blocker{Type: "requirement-decision", ID: requirement.ID})
		}
	}
	for _, question := range validated.Proposal.Questions {
		resolution, exists := resolutions[question.ID]
		if exists && resolution.CandidateDigest != candidateDigestForQuestion(question) {
			report.Summary.Unresolved++
			report.Summary.Stale++
			report.Blockers = append(report.Blockers, Blocker{Type: "question-review-stale", ID: question.ID})
		} else if exists {
			report.Summary.Resolved++
		} else {
			report.Summary.Unresolved++
			report.Blockers = append(report.Blockers, Blocker{Type: "question-resolution", ID: question.ID})
		}
	}
	if len(report.Blockers) > 0 {
		report.State = "blocked"
		report.Next = "review"
	}
	return report, nil
}

// LoadReviewedInput returns only decisions that are current and accepted. It is
// intentionally available to the materializer so provider output cannot revive
// rejected or stale requirements.
func LoadReviewedInput(projectRoot, proposalPath, reviewPath string) (*ReviewedInput, error) {
	status, err := Status(projectRoot, proposalPath, reviewPath)
	if err != nil {
		return nil, err
	}
	if status.State != "ready" {
		return nil, errors.New("review is not ready for materialization")
	}
	validated, err := ValidateProposal(projectRoot, proposalPath)
	if err != nil {
		return nil, err
	}
	root, err := normalizedRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	review, normalizedReviewPath, err := readReview(root, reviewPath, validated)
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, errors.New("review does not exist")
	}
	reviewBytes, _, err := readProjectFile(root, normalizedReviewPath, "review")
	if err != nil {
		return nil, err
	}
	decisions := map[string]Decision{}
	for _, decision := range review.Decisions {
		decisions[decision.ID] = decision
	}
	accepted := []Requirement{}
	for _, requirement := range validated.Proposal.Requirements {
		decision := decisions[requirement.ID]
		if decision.Decision == "accepted" && decision.CandidateDigest == candidateDigestForRequirement(requirement) {
			accepted = append(accepted, requirement)
		}
	}
	answers := map[string]string{}
	for _, resolution := range review.Resolutions {
		answers[resolution.ID] = resolution.Answer
	}
	return &ReviewedInput{
		Proposal:     validated,
		ReviewPath:   normalizedReviewPath,
		ReviewDigest: rawSHA256(reviewBytes),
		Accepted:     accepted,
		Answers:      answers,
	}, nil
}
