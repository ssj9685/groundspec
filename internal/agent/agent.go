package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const probeTimeout = 5 * time.Second

type Descriptor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Executable string `json:"executable,omitempty"`
	Version    string `json:"version,omitempty"`
	Auth       string `json:"auth"`
	Ready      bool   `json:"ready"`
	Reason     string `json:"reason,omitempty"`
}

// Public returns the portable, publication-safe adapter provenance stored in
// repository artifacts. The full descriptor remains available to the running
// process so it can execute the discovered binary, but local paths and the
// authentication mechanism never need to leave the machine.
func (descriptor Descriptor) Public() Descriptor {
	public := descriptor
	if public.Executable != "" {
		public.Executable = filepath.Base(public.Executable)
	}
	if public.Ready {
		public.Auth = "authenticated"
	} else if public.Auth != "unknown" {
		public.Auth = "not-authenticated"
	}
	public.Reason = ""
	return public
}

// ValidatePublic rejects runtime-only adapter details in persisted protocol
// files. Persisted adapters describe portable provenance, not how to find or
// authenticate the executable on the current machine.
func (descriptor Descriptor) ValidatePublic() error {
	if strings.TrimSpace(descriptor.ID) == "" || strings.TrimSpace(descriptor.Name) == "" || strings.TrimSpace(descriptor.Kind) == "" {
		return errors.New("adapter id, name, and kind are required")
	}
	if !descriptor.Ready || descriptor.Auth != "authenticated" {
		return errors.New("persisted adapter must record generic authenticated readiness")
	}
	if descriptor.Reason != "" {
		return errors.New("persisted adapter must not contain local probe details")
	}
	if descriptor.Executable != "" {
		if descriptor.Executable != filepath.Base(descriptor.Executable) || strings.ContainsAny(descriptor.Executable, `/\`) {
			return errors.New("persisted adapter executable must be a portable basename")
		}
	}
	return nil
}

type Request struct {
	Prompt          string
	Schema          []byte
	Workdir         string
	ReadOnly        bool
	AllowExec       bool
	ReasoningEffort string
	Timeout         time.Duration
}

type Adapter interface {
	Descriptor() Descriptor
	Run(context.Context, Request) ([]byte, error)
}

type commandRunner interface {
	LookPath(string) (string, error)
	Output(context.Context, string, ...string) ([]byte, error)
}

type osRunner struct{}

func (osRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (osRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	return command.CombinedOutput()
}

func probeLine(output []byte, prefix string) string {
	wanted := strings.ToLower(prefix)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), wanted) {
			return line
		}
	}
	return ""
}

func authenticatedLoginStatus(output []byte) string {
	const prefix = "logged in using "
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if len(line) > len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) && strings.TrimSpace(line[len(prefix):]) != "" {
			return line
		}
	}
	return ""
}

func discoverWith(ctx context.Context, runner commandRunner) []Descriptor {
	descriptors := []Descriptor{}
	executable, err := runner.LookPath("codex")
	if err != nil {
		return descriptors
	}
	descriptor := Descriptor{ID: "codex-cli", Name: "Codex CLI", Kind: "agent-cli", Executable: executable, Auth: "unknown"}
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	version, versionErr := runner.Output(probeContext, executable, "--version")
	cancel()
	if versionErr != nil {
		descriptor.Reason = "version probe failed"
		descriptors = append(descriptors, descriptor)
		return descriptors
	}
	descriptor.Version = probeLine(version, "codex-cli ")
	if descriptor.Version == "" {
		descriptor.Reason = "incompatible version response"
		descriptors = append(descriptors, descriptor)
		return descriptors
	}
	loginContext, cancel := context.WithTimeout(ctx, probeTimeout)
	login, loginErr := runner.Output(loginContext, executable, "login", "status")
	cancel()
	loginStatus := authenticatedLoginStatus(login)
	if loginErr == nil && loginStatus != "" {
		descriptor.Auth = loginStatus
		descriptor.Ready = true
	} else {
		descriptor.Auth = "not-authenticated"
		descriptor.Reason = "login status did not confirm authentication"
	}
	descriptors = append(descriptors, descriptor)
	return descriptors
}

func Discover(ctx context.Context) []Descriptor {
	return discoverWith(ctx, osRunner{})
}

func Resolve(ctx context.Context, selection string) (Adapter, error) {
	descriptor, err := selectDescriptor(Discover(ctx), selection)
	if err != nil {
		return nil, err
	}
	return adapterFor(descriptor)
}

func selectDescriptor(descriptors []Descriptor, selection string) (Descriptor, error) {
	if selection == "" {
		selection = "auto"
	}
	ready := make([]Descriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Ready {
			ready = append(ready, descriptor)
		}
	}
	sort.Slice(ready, func(left, right int) bool { return ready[left].ID < ready[right].ID })
	if selection == "auto" {
		switch len(ready) {
		case 0:
			return Descriptor{}, errors.New("no authenticated compatible agent CLI is available")
		case 1:
			return ready[0], nil
		default:
			ids := make([]string, len(ready))
			for index, descriptor := range ready {
				ids[index] = descriptor.ID
			}
			return Descriptor{}, fmt.Errorf("multiple authenticated adapters are available; select one explicitly: %s", strings.Join(ids, ", "))
		}
	}
	for _, descriptor := range descriptors {
		if descriptor.ID != selection {
			continue
		}
		if !descriptor.Ready {
			return Descriptor{}, fmt.Errorf("adapter %s is not ready: %s", selection, descriptor.Reason)
		}
		return descriptor, nil
	}
	return Descriptor{}, fmt.Errorf("unknown adapter: %s", selection)
}

func adapterFor(descriptor Descriptor) (Adapter, error) {
	switch descriptor.ID {
	case "codex-cli":
		return &codexAdapter{descriptor: descriptor}, nil
	default:
		return nil, fmt.Errorf("unsupported adapter: %s", descriptor.ID)
	}
}

type codexAdapter struct {
	descriptor Descriptor
}

func (adapter *codexAdapter) Descriptor() Descriptor { return adapter.descriptor }

func (adapter *codexAdapter) Run(ctx context.Context, request Request) ([]byte, error) {
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, errors.New("agent prompt must not be blank")
	}
	if !json.Valid(request.Schema) {
		return nil, errors.New("agent output schema must be valid JSON")
	}
	if !request.ReadOnly && !request.AllowExec {
		return nil, errors.New("workspace-writing agent requests require an explicit execution action")
	}
	workdir, err := filepath.Abs(request.Workdir)
	if err != nil {
		return nil, err
	}
	if request.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	temporary, err := os.MkdirTemp("", "groundspec-agent-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)
	schemaPath := filepath.Join(temporary, "schema.json")
	outputPath := filepath.Join(temporary, "output.json")
	if err := os.WriteFile(schemaPath, request.Schema, 0o600); err != nil {
		return nil, err
	}
	sandbox := "workspace-write"
	if request.ReadOnly {
		sandbox = "read-only"
	}
	arguments := []string{"exec", "--ephemeral", "--skip-git-repo-check", "--color", "never", "--output-schema", schemaPath, "-o", outputPath, "-C", workdir, "-s", sandbox}
	if request.ReasoningEffort != "" {
		switch request.ReasoningEffort {
		case "low", "medium", "high", "xhigh":
			arguments = append(arguments, "-c", "model_reasoning_effort=\""+request.ReasoningEffort+"\"")
		default:
			return nil, fmt.Errorf("unsupported reasoning effort: %s", request.ReasoningEffort)
		}
	}
	arguments = append(arguments, "-")
	command := exec.CommandContext(ctx, adapter.descriptor.Executable, arguments...)
	command.Stdin = strings.NewReader(request.Prompt)
	var stderr bytes.Buffer
	command.Stdout = &stderr
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("adapter %s exceeded its %s timeout", adapter.descriptor.ID, request.Timeout)
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("adapter %s failed: %s", adapter.descriptor.ID, message)
	}
	result, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("adapter %s did not produce structured output: %w", adapter.descriptor.ID, err)
	}
	if !json.Valid(result) {
		return nil, fmt.Errorf("adapter %s produced invalid JSON", adapter.descriptor.ID)
	}
	return result, nil
}
