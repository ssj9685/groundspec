package core

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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	graphVersion = 1
	proofVersion = 1
)

type ValidationError struct {
	Message string
	Cause   error
}

func (err *ValidationError) Error() string { return err.Message }
func (err *ValidationError) Unwrap() error { return err.Cause }

func invalid(format string, arguments ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, arguments...)}
}

func invalidCause(cause error, format string, arguments ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, arguments...), Cause: cause}
}

type Frame struct {
	Label string
	Value []byte
}

func DigestFrames(frames []Frame) string {
	hash := sha256.New()
	var labelLength [4]byte
	var valueLength [8]byte
	for _, frame := range frames {
		label := []byte(frame.Label)
		binary.BigEndian.PutUint32(labelLength[:], uint32(len(label)))
		binary.BigEndian.PutUint64(valueLength[:], uint64(len(frame.Value)))
		_, _ = hash.Write(labelLength[:])
		_, _ = hash.Write(label)
		_, _ = hash.Write(valueLength[:])
		_, _ = hash.Write(frame.Value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type Node struct {
	ID        string
	Kind      string
	Inputs    []string
	DependsOn []string
	Proof     *string
}

type Graph struct {
	Root      string
	GraphPath string
	Version   int
	Nodes     []Node
	ByID      map[string]*Node
}

type graphDocument struct {
	Version int            `json:"version"`
	Nodes   []nodeDocument `json:"nodes"`
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

type nodeDocument struct {
	ID        string         `json:"id"`
	Kind      optionalString `json:"kind"`
	Inputs    []string       `json:"inputs"`
	DependsOn []string       `json:"dependsOn"`
	Proof     optionalString `json:"proof"`
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

func assertRelativePath(value, label string) error {
	if value == "" {
		return invalid("%s must be a non-empty path string", label)
	}
	if filepath.IsAbs(value) {
		return invalid("%s must be relative to the project root", label)
	}
	return nil
}

func resolveInside(root, relativePath, label string) (string, error) {
	if err := assertRelativePath(relativePath, label); err != nil {
		return "", err
	}
	absolutePath := filepath.Join(root, filepath.FromSlash(relativePath))
	relation, err := filepath.Rel(root, absolutePath)
	if err != nil {
		return "", invalidCause(err, "cannot resolve %s", label)
	}
	if relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) || filepath.IsAbs(relation) {
		return "", invalid("%s resolves outside the project root: %s", label, relativePath)
	}
	return absolutePath, nil
}

func normalizePath(value string) string {
	return filepath.ToSlash(value)
}

func assertNoSymlinkParents(root, absolutePath, label string) error {
	relation, err := filepath.Rel(root, absolutePath)
	if err != nil {
		return invalidCause(err, "cannot inspect %s", label)
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
			relative, _ := filepath.Rel(root, current)
			return invalid("%s traverses symbolic-link directory: %s", label, normalizePath(relative))
		}
	}
	return nil
}

func assertPathExists(root, relativePath, label string) (string, error) {
	absolutePath, err := resolveInside(root, relativePath, label)
	if err != nil {
		return "", err
	}
	if err := assertNoSymlinkParents(root, absolutePath, label); err != nil {
		return "", err
	}
	if _, err := os.Lstat(absolutePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", invalidCause(err, "%s does not exist: %s", label, relativePath)
		}
		return "", err
	}
	return absolutePath, nil
}

func LoadGraph(projectRoot, graphPath string) (*Graph, error) {
	absoluteRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, invalidCause(err, "cannot resolve project root: %s", projectRoot)
	}
	root, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, invalidCause(err, "project root does not exist: %s", projectRoot)
	}
	absoluteGraphPath, err := resolveInside(root, graphPath, "graph path")
	if err != nil {
		return nil, err
	}
	if err := assertNoSymlinkParents(root, absoluteGraphPath, "graph path"); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absoluteGraphPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, invalidCause(err, "graph does not exist: %s", absoluteGraphPath)
	}
	if err != nil {
		return nil, err
	}
	var document graphDocument
	if err := decodeStrict(data, &document); err != nil {
		return nil, invalidCause(err, "graph is not valid JSON: %v", err)
	}
	if document.Version != graphVersion {
		return nil, invalid("graph version must be %d", graphVersion)
	}
	if len(document.Nodes) == 0 {
		return nil, invalid("graph must contain at least one node")
	}

	graph := &Graph{
		Root:      root,
		GraphPath: normalizePath(mustRelative(root, absoluteGraphPath)),
		Version:   graphVersion,
		Nodes:     make([]Node, len(document.Nodes)),
		ByID:      make(map[string]*Node, len(document.Nodes)),
	}
	for index, candidate := range document.Nodes {
		label := fmt.Sprintf("node at index %d", index)
		if candidate.ID == "" {
			return nil, invalid("%s must have a non-empty id", label)
		}
		if candidate.Kind.Null {
			return nil, invalid("node '%s' kind must be a string", candidate.ID)
		}
		if candidate.Inputs == nil {
			return nil, invalid("node '%s' inputs must be an array", candidate.ID)
		}
		for inputIndex, input := range candidate.Inputs {
			if err := assertRelativePath(input, fmt.Sprintf("node '%s' input %d", candidate.ID, inputIndex)); err != nil {
				return nil, err
			}
		}
		seenDependencies := map[string]bool{}
		for _, dependency := range candidate.DependsOn {
			if dependency == "" {
				return nil, invalid("node '%s' has an invalid dependency id", candidate.ID)
			}
			if seenDependencies[dependency] {
				return nil, invalid("node '%s' contains duplicate dependencies", candidate.ID)
			}
			seenDependencies[dependency] = true
		}
		var proof *string
		if candidate.Proof.Present {
			if candidate.Proof.Null {
				return nil, invalid("node '%s' proof must be a non-empty path string", candidate.ID)
			}
			if err := assertRelativePath(candidate.Proof.Value, fmt.Sprintf("node '%s' proof", candidate.ID)); err != nil {
				return nil, err
			}
			proofValue := candidate.Proof.Value
			proof = &proofValue
		}
		graph.Nodes[index] = Node{
			ID:        candidate.ID,
			Kind:      candidate.Kind.Value,
			Inputs:    append([]string(nil), candidate.Inputs...),
			DependsOn: append([]string(nil), candidate.DependsOn...),
			Proof:     proof,
		}
		if _, exists := graph.ByID[candidate.ID]; exists {
			return nil, invalid("graph contains duplicate node id '%s'", candidate.ID)
		}
		graph.ByID[candidate.ID] = &graph.Nodes[index]
	}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		for _, dependency := range node.DependsOn {
			if graph.ByID[dependency] == nil {
				return nil, invalid("node '%s' depends on missing node '%s'", node.ID, dependency)
			}
		}
	}

	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(*Node, []string) error
	visit = func(node *Node, trail []string) error {
		if visiting[node.ID] {
			return invalid("graph contains dependency cycle: %s", strings.Join(append(trail, node.ID), " -> "))
		}
		if visited[node.ID] {
			return nil
		}
		visiting[node.ID] = true
		for _, dependency := range node.DependsOn {
			if err := visit(graph.ByID[dependency], append(trail, node.ID)); err != nil {
				return err
			}
		}
		delete(visiting, node.ID)
		visited[node.ID] = true
		return nil
	}
	for index := range graph.Nodes {
		if err := visit(&graph.Nodes[index], nil); err != nil {
			return nil, err
		}
	}

	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		inputPaths := make([]string, 0, len(node.Inputs))
		for inputIndex, input := range node.Inputs {
			absoluteInput, err := assertPathExists(root, input, fmt.Sprintf("node '%s' input %d", node.ID, inputIndex))
			if err != nil {
				return nil, err
			}
			inputPaths = append(inputPaths, absoluteInput)
		}
		if node.Proof == nil {
			continue
		}
		proofPath, err := resolveInside(root, *node.Proof, fmt.Sprintf("node '%s' proof", node.ID))
		if err != nil {
			return nil, err
		}
		if err := assertNoSymlinkParents(root, proofPath, fmt.Sprintf("node '%s' proof", node.ID)); err != nil {
			return nil, err
		}
		for inputIndex, inputPath := range inputPaths {
			relation, err := filepath.Rel(inputPath, proofPath)
			if err != nil {
				return nil, err
			}
			if relation == "." || (!strings.HasPrefix(relation, "..") && !filepath.IsAbs(relation)) {
				return nil, invalid("node '%s' proof must not be contained by its own input '%s'", node.ID, normalizePath(node.Inputs[inputIndex]))
			}
		}
	}
	return graph, nil
}

func mustRelative(root, target string) string {
	relation, err := filepath.Rel(root, target)
	if err != nil {
		panic(err)
	}
	return relation
}

func digestAbsolutePath(root, absolutePath string) (string, error) {
	info, err := os.Lstat(absolutePath)
	if err != nil {
		return "", err
	}
	relativePath := normalizePath(mustRelative(root, absolutePath))
	frames := []Frame{
		{Label: "domain", Value: []byte("groundspec/path/v1")},
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(absolutePath)
		if err != nil {
			return "", err
		}
		frames = append(frames,
			Frame{Label: "type", Value: []byte("symlink")},
			Frame{Label: "path", Value: []byte(relativePath)},
			Frame{Label: "target", Value: []byte(target)},
		)
	case info.Mode().IsRegular():
		contents, err := os.ReadFile(absolutePath)
		if err != nil {
			return "", err
		}
		frames = append(frames,
			Frame{Label: "type", Value: []byte("file")},
			Frame{Label: "path", Value: []byte(relativePath)},
			Frame{Label: "content", Value: contents},
		)
	case info.IsDir():
		entries, err := os.ReadDir(absolutePath)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if !utf8.ValidString(entry.Name()) {
				return "", invalid("directory contains a non-UTF-8 entry: %s", relativePath)
			}
		}
		sort.Slice(entries, func(left, right int) bool {
			return bytes.Compare([]byte(entries[left].Name()), []byte(entries[right].Name())) < 0
		})
		frames = append(frames,
			Frame{Label: "type", Value: []byte("directory")},
			Frame{Label: "path", Value: []byte(relativePath)},
		)
		for _, entry := range entries {
			childDigest, err := digestAbsolutePath(root, filepath.Join(absolutePath, entry.Name()))
			if err != nil {
				return "", err
			}
			frames = append(frames,
				Frame{Label: "entry-name", Value: []byte(entry.Name())},
				Frame{Label: "entry-digest", Value: []byte(childDigest)},
			)
		}
	default:
		return "", invalid("unsupported input type: %s", relativePath)
	}
	return DigestFrames(frames), nil
}

func digestRelativePath(graph *Graph, relativePath, label string) (string, error) {
	absolutePath, err := assertPathExists(graph.Root, relativePath, label)
	if err != nil {
		return "", err
	}
	return digestAbsolutePath(graph.Root, absolutePath)
}

func ComputeNodeDigests(graph *Graph) (map[string]string, error) {
	memo := map[string]string{}
	var compute func(*Node) (string, error)
	compute = func(node *Node) (string, error) {
		if digest, exists := memo[node.ID]; exists {
			return digest, nil
		}
		frames := []Frame{
			{Label: "domain", Value: []byte("groundspec/node/v1")},
			{Label: "graph-version", Value: []byte(strconv.Itoa(graph.Version))},
			{Label: "id", Value: []byte(node.ID)},
			{Label: "kind", Value: []byte(node.Kind)},
		}
		inputs := append([]string(nil), node.Inputs...)
		sort.Slice(inputs, func(left, right int) bool {
			return bytes.Compare([]byte(normalizePath(inputs[left])), []byte(normalizePath(inputs[right]))) < 0
		})
		for _, input := range inputs {
			digest, err := digestRelativePath(graph, input, fmt.Sprintf("node '%s' input", node.ID))
			if err != nil {
				return "", err
			}
			frames = append(frames,
				Frame{Label: "input-path", Value: []byte(normalizePath(input))},
				Frame{Label: "input-digest", Value: []byte(digest)},
			)
		}
		dependencies := append([]string(nil), node.DependsOn...)
		sort.Slice(dependencies, func(left, right int) bool {
			return bytes.Compare([]byte(dependencies[left]), []byte(dependencies[right])) < 0
		})
		for _, dependency := range dependencies {
			digest, err := compute(graph.ByID[dependency])
			if err != nil {
				return "", err
			}
			frames = append(frames,
				Frame{Label: "dependency-id", Value: []byte(dependency)},
				Frame{Label: "dependency-digest", Value: []byte(digest)},
			)
		}
		digest := DigestFrames(frames)
		memo[node.ID] = digest
		return digest, nil
	}
	for index := range graph.Nodes {
		if _, err := compute(&graph.Nodes[index]); err != nil {
			return nil, err
		}
	}
	return memo, nil
}

type Evidence struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type Proof struct {
	Version    int       `json:"version"`
	Node       string    `json:"node"`
	Digest     string    `json:"digest"`
	Result     string    `json:"result"`
	RecordedAt string    `json:"recordedAt"`
	Evidence   *Evidence `json:"evidence,omitempty"`
	Note       *string   `json:"note,omitempty"`
}

type proofDocument struct {
	Version    int             `json:"version"`
	Node       string          `json:"node"`
	Digest     string          `json:"digest"`
	Result     string          `json:"result"`
	RecordedAt string          `json:"recordedAt"`
	Evidence   json.RawMessage `json:"evidence"`
	Note       optionalString  `json:"note"`
}

func isDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func parseProof(data []byte, node *Node) (*Proof, error) {
	var document proofDocument
	if err := decodeStrict(data, &document); err != nil {
		return nil, invalidCause(err, "proof for node '%s' is not valid JSON", node.ID)
	}
	if document.Note.Null {
		return nil, invalid("proof for node '%s' note must be a string", node.ID)
	}
	proof := &Proof{
		Version:    document.Version,
		Node:       document.Node,
		Digest:     document.Digest,
		Result:     document.Result,
		RecordedAt: document.RecordedAt,
	}
	if document.Note.Present {
		note := document.Note.Value
		proof.Note = &note
	}
	if len(document.Evidence) > 0 {
		if bytes.Equal(document.Evidence, []byte("null")) {
			return nil, invalid("proof for node '%s' evidence must be an object", node.ID)
		}
		var evidence Evidence
		if err := decodeStrict(document.Evidence, &evidence); err != nil {
			return nil, invalidCause(err, "proof for node '%s' evidence must be an object", node.ID)
		}
		proof.Evidence = &evidence
	}
	if proof.Version != proofVersion {
		return nil, invalid("proof for node '%s' version must be %d", node.ID, proofVersion)
	}
	if proof.Node != node.ID {
		return nil, invalid("proof at '%s' belongs to '%s', not '%s'", *node.Proof, proof.Node, node.ID)
	}
	if !isDigest(proof.Digest) {
		return nil, invalid("proof for node '%s' has an invalid digest", node.ID)
	}
	if proof.Result != "passed" && proof.Result != "failed" {
		return nil, invalid("proof for node '%s' result must be 'passed' or 'failed'", node.ID)
	}
	if _, err := time.Parse(time.RFC3339Nano, proof.RecordedAt); err != nil {
		return nil, invalidCause(err, "proof for node '%s' has an invalid recordedAt value", node.ID)
	}
	if proof.Evidence != nil {
		if err := assertRelativePath(proof.Evidence.Path, fmt.Sprintf("proof for node '%s' evidence path", node.ID)); err != nil {
			return nil, err
		}
		if !isDigest(proof.Evidence.Digest) {
			return nil, invalid("proof for node '%s' evidence has an invalid digest", node.ID)
		}
	}
	return proof, nil
}

func readProof(graph *Graph, node *Node) (*Proof, error) {
	absolutePath, err := resolveInside(graph.Root, *node.Proof, fmt.Sprintf("node '%s' proof", node.ID))
	if err != nil {
		return nil, err
	}
	if err := assertNoSymlinkParents(graph.Root, absolutePath, fmt.Sprintf("node '%s' proof", node.ID)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolutePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseProof(data, node)
}

type NodeReport struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Digest     string `json:"digest"`
	Proof      string `json:"proof,omitempty"`
	RecordedAt string `json:"recordedAt,omitempty"`
}

type Report struct {
	Version int          `json:"version"`
	Graph   string       `json:"graph"`
	Overall string       `json:"overall"`
	Nodes   []NodeReport `json:"nodes"`
}

func EvaluateGraph(graph *Graph) (Report, error) {
	digests, err := ComputeNodeDigests(graph)
	if err != nil {
		return Report{}, err
	}
	report := Report{Version: 1, Graph: graph.GraphPath, Nodes: make([]NodeReport, 0, len(graph.Nodes))}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		result := NodeReport{ID: node.ID, Kind: node.Kind, Digest: digests[node.ID]}
		if node.Proof == nil {
			result.Status = "declared"
			report.Nodes = append(report.Nodes, result)
			continue
		}
		result.Proof = *node.Proof
		proof, err := readProof(graph, node)
		if err != nil {
			return Report{}, err
		}
		if proof == nil {
			result.Status = "missing"
			report.Nodes = append(report.Nodes, result)
			continue
		}
		if proof.Digest != result.Digest {
			result.Status = "stale"
			result.Reason = "node-digest-changed"
			report.Nodes = append(report.Nodes, result)
			continue
		}
		if proof.Evidence != nil {
			currentDigest, err := digestRelativePath(graph, proof.Evidence.Path, fmt.Sprintf("proof for node '%s' evidence", node.ID))
			if errors.Is(err, fs.ErrNotExist) {
				result.Status = "stale"
				result.Reason = "evidence-missing"
				report.Nodes = append(report.Nodes, result)
				continue
			}
			if err != nil {
				return Report{}, err
			}
			if currentDigest != proof.Evidence.Digest {
				result.Status = "stale"
				result.Reason = "evidence-changed"
				report.Nodes = append(report.Nodes, result)
				continue
			}
		}
		result.Status = proof.Result
		result.RecordedAt = proof.RecordedAt
		report.Nodes = append(report.Nodes, result)
	}
	report.Overall = "healthy"
	for _, node := range report.Nodes {
		if node.Status == "failed" {
			report.Overall = "failed"
			break
		}
		if node.Status == "missing" || node.Status == "stale" {
			report.Overall = "incomplete"
		}
	}
	return report, nil
}

type AttestOptions struct {
	Result   string
	Evidence *string
	Note     *string
}

func AttestNode(graph *Graph, nodeID string, options AttestOptions) (Proof, error) {
	node := graph.ByID[nodeID]
	if node == nil {
		return Proof{}, invalid("cannot attest missing node '%s'", nodeID)
	}
	if node.Proof == nil {
		return Proof{}, invalid("node '%s' does not declare a proof path", nodeID)
	}
	if options.Result != "passed" && options.Result != "failed" {
		return Proof{}, invalid("attestation result must be 'passed' or 'failed'")
	}
	digests, err := ComputeNodeDigests(graph)
	if err != nil {
		return Proof{}, err
	}
	proof := Proof{
		Version:    proofVersion,
		Node:       node.ID,
		Digest:     digests[node.ID],
		Result:     options.Result,
		RecordedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Note:       options.Note,
	}
	if options.Evidence != nil {
		digest, err := digestRelativePath(graph, *options.Evidence, fmt.Sprintf("attestation evidence for node '%s'", node.ID))
		if err != nil {
			return Proof{}, err
		}
		proof.Evidence = &Evidence{Path: normalizePath(*options.Evidence), Digest: digest}
	}
	absoluteProofPath, err := resolveInside(graph.Root, *node.Proof, fmt.Sprintf("node '%s' proof", node.ID))
	if err != nil {
		return Proof{}, err
	}
	if err := assertNoSymlinkParents(graph.Root, absoluteProofPath, fmt.Sprintf("node '%s' proof", node.ID)); err != nil {
		return Proof{}, err
	}
	proofDirectory := filepath.Dir(absoluteProofPath)
	if err := os.MkdirAll(proofDirectory, 0o755); err != nil {
		return Proof{}, err
	}
	temporary, err := os.CreateTemp(proofDirectory, "."+filepath.Base(absoluteProofPath)+".*.tmp")
	if err != nil {
		return Proof{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return Proof{}, err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(proof); err != nil {
		_ = temporary.Close()
		return Proof{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Proof{}, err
	}
	if err := temporary.Close(); err != nil {
		return Proof{}, err
	}
	if err := os.Rename(temporaryPath, absoluteProofPath); err != nil {
		return Proof{}, err
	}
	return proof, nil
}
