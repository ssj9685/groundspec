package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	path    string
	outputs map[string][]byte
	errors  map[string]error
}

func (runner fakeRunner) LookPath(string) (string, error) {
	if runner.path == "" {
		return "", errors.New("missing")
	}
	return runner.path, nil
}

func (runner fakeRunner) Output(_ context.Context, _ string, arguments ...string) ([]byte, error) {
	key := ""
	for _, argument := range arguments {
		if key != "" {
			key += " "
		}
		key += argument
	}
	return runner.outputs[key], runner.errors[key]
}

func TestPB14DiscoveryUsesOnlyCapabilityAndLoginStatusProbes(t *testing.T) {
	runner := fakeRunner{
		path: "/tools/codex",
		outputs: map[string][]byte{
			"--version":    []byte("WARNING: PATH alias unavailable\ncodex-cli 1.2.3\n"),
			"login status": []byte("WARNING: PATH alias unavailable\nLogged in using ChatGPT\n"),
		},
		errors: map[string]error{},
	}
	got := discoverWith(context.Background(), runner)
	want := []Descriptor{{
		ID: "codex-cli", Name: "Codex CLI", Kind: "agent-cli", Executable: "/tools/codex",
		Version: "codex-cli 1.2.3", Auth: "Logged in using ChatGPT", Ready: true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("descriptors = %#v, want %#v", got, want)
	}
}

func TestPB29PublicDescriptorRemovesLocalExecutionAndAuthDetails(t *testing.T) {
	descriptor := Descriptor{
		ID:         "codex-cli",
		Name:       "Codex CLI",
		Kind:       "agent-cli",
		Executable: filepath.Join(string(filepath.Separator), "private", "user", "bin", "codex"),
		Version:    "codex-cli 1.2.3",
		Auth:       "Logged in using a local account",
		Ready:      true,
		Reason:     "local probe detail",
	}

	public := descriptor.Public()
	if public.Executable != "codex" || public.Auth != "authenticated" || public.Reason != "" {
		t.Fatalf("descriptor was not minimized for publication: %#v", public)
	}
	if descriptor.Executable == public.Executable {
		t.Fatal("runtime descriptor was unexpectedly mutated")
	}
	if err := public.ValidatePublic(); err != nil {
		t.Fatalf("public descriptor was rejected: %v", err)
	}
}

func TestPB29PersistedDescriptorRejectsRuntimeOnlyDetails(t *testing.T) {
	valid := Descriptor{ID: "codex-cli", Name: "Codex CLI", Kind: "agent-cli", Executable: "codex", Auth: "authenticated", Ready: true}
	for name, mutate := range map[string]func(*Descriptor){
		"local executable": func(descriptor *Descriptor) { descriptor.Executable = "/opt/example/bin/codex" },
		"login mechanism":  func(descriptor *Descriptor) { descriptor.Auth = "Logged in using ChatGPT" },
		"probe reason":     func(descriptor *Descriptor) { descriptor.Reason = "read from local probe" },
		"not ready":        func(descriptor *Descriptor) { descriptor.Ready = false },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.ValidatePublic(); err == nil {
				t.Fatalf("runtime-only descriptor was accepted: %#v", candidate)
			}
		})
	}
}

func TestPB14MissingCLIIsNotInvented(t *testing.T) {
	if got := discoverWith(context.Background(), fakeRunner{}); len(got) != 0 {
		t.Fatalf("descriptors = %#v, want none", got)
	}
}

func TestPB14DiscoveryRequiresCompatibleVersionAndConfirmedLogin(t *testing.T) {
	for name, runner := range map[string]fakeRunner{
		"incompatible version": {
			path:    "/tools/codex",
			outputs: map[string][]byte{"--version": []byte("another-cli 1.0\n")},
			errors:  map[string]error{},
		},
		"unconfirmed login": {
			path:    "/tools/codex",
			outputs: map[string][]byte{"--version": []byte("codex-cli 1.2.3\n"), "login status": []byte("Not logged in\n")},
			errors:  map[string]error{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			descriptors := discoverWith(context.Background(), runner)
			if len(descriptors) != 1 || descriptors[0].Ready {
				t.Fatalf("descriptors = %#v", descriptors)
			}
		})
	}
}

func TestPB15SelectionIsExplicitOrUnambiguous(t *testing.T) {
	readyA := Descriptor{ID: "agent-a", Ready: true}
	readyB := Descriptor{ID: "agent-b", Ready: true}
	notReady := Descriptor{ID: "agent-c", Ready: false, Reason: "not authenticated"}

	selected, err := selectDescriptor([]Descriptor{readyB, notReady, readyA}, "agent-b")
	if err != nil || selected.ID != "agent-b" {
		t.Fatalf("explicit selection = %#v, %v", selected, err)
	}
	if _, err := selectDescriptor([]Descriptor{readyA, readyB}, "auto"); err == nil {
		t.Fatal("ambiguous automatic selection succeeded")
	}
	selected, err = selectDescriptor([]Descriptor{notReady, readyA}, "auto")
	if err != nil || selected.ID != "agent-a" {
		t.Fatalf("unambiguous automatic selection = %#v, %v", selected, err)
	}
	if _, err := selectDescriptor([]Descriptor{notReady}, "auto"); err == nil {
		t.Fatal("automatic selection succeeded without a ready adapter")
	}
	if _, err := selectDescriptor([]Descriptor{notReady}, "agent-c"); err == nil {
		t.Fatal("explicit selection accepted an ineligible adapter")
	}
}

func TestPB18WorkspaceWriteDoesNotRequestConflictingApprovalMode(t *testing.T) {
	// Codex CLI 0.147 rejects --approve-for-me together with --sandbox. The
	// adapter relies on noninteractive exec plus workspace-write instead.
	source, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(source, []byte("--approve-for-me")) {
		t.Fatal("adapter must not combine sandbox mode with --approve-for-me")
	}
}

func TestPB16CodexAdapterSupportsANewNonGitWorkspace(t *testing.T) {
	source, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(source, []byte("--skip-git-repo-check")) {
		t.Fatal("adapter must support start and init before a workspace has Git metadata")
	}
}

func TestPB18WorkspaceWritingRequiresExplicitExecutionAction(t *testing.T) {
	adapter := &codexAdapter{}
	_, err := adapter.Run(context.Background(), Request{Prompt: "write files", Schema: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "explicit execution action") {
		t.Fatalf("implicit workspace-write error = %v", err)
	}
}
