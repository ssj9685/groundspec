package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ssj9685/groundspec/internal/agent"
	"github.com/ssj9685/groundspec/internal/intake"
	"github.com/ssj9685/groundspec/internal/pipeline"
	"github.com/ssj9685/groundspec/internal/workflow"
)

const usageText = "usage: groundspec adapters [--json] | groundspec start <source> --output <directory> [--adapter id] [--json] | groundspec init <source> [--output directory] [--adapter id] [--json] | groundspec ingest <source> [--json] | groundspec draft <bundle> --output <proposal> [--adapter id] [--json] | groundspec proposal validate <proposal> [--json] | groundspec review <proposal> [--review path] (--accept id | --reject id | --resolve id --answer text) [--note text] [--json] | groundspec status <proposal> [--review path] [--json] | groundspec materialize <proposal> [--review path] [--output path] [--adapter id] [--json] | groundspec implement <plan> [--output path] [--adapter id] [--json] | groundspec verify <plan> [--implementation path] [--output path] [--json] | groundspec lifecycle [--proposal path] [--review path] [--plan path] [--implementation path] [--verification path] [--graph path] [--json] | groundspec check [--graph path] [--json] | groundspec attest <node> --result passed|failed [--evidence path] [--note text] [--graph path] [--json]"

const defaultReviewPath = ".groundspec/review.json"

var Version = "dev"

type UsageError struct {
	Message string
}

func (err *UsageError) Error() string { return err.Message }

type optionKind int

const (
	valueOption optionKind = iota
	booleanOption
)

func parseOptions(arguments []string, allowed map[string]optionKind) (map[string]string, error) {
	options := map[string]string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if len(argument) < 3 || argument[:2] != "--" {
			return nil, &UsageError{Message: fmt.Sprintf("unexpected argument '%s'", argument)}
		}
		name := argument[2:]
		kind, exists := allowed[name]
		if !exists {
			return nil, &UsageError{Message: fmt.Sprintf("unknown option '--%s'", name)}
		}
		if kind == booleanOption {
			options[name] = "true"
			continue
		}
		if index+1 >= len(arguments) || (len(arguments[index+1]) >= 2 && arguments[index+1][:2] == "--") {
			return nil, &UsageError{Message: fmt.Sprintf("option '--%s' requires a value", name)}
		}
		options[name] = arguments[index+1]
		index++
	}
	return options, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeHumanReport(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintf(writer, "groundspec: %s\n", report.Overall); err != nil {
		return err
	}
	symbols := map[string]string{
		"declared": "·",
		"passed":   "✓",
		"failed":   "✗",
		"missing":  "?",
		"stale":    "!",
	}
	for _, node := range report.Nodes {
		reason := ""
		if node.Reason != "" {
			reason = " (" + node.Reason + ")"
		}
		if _, err := fmt.Fprintf(writer, "%s %s: %s%s\n", symbols[node.Status], node.ID, node.Status, reason); err != nil {
			return err
		}
	}
	return nil
}

func writeWorkflowStatus(writer io.Writer, report workflow.StatusReport) error {
	if _, err := fmt.Fprintf(writer, "groundspec workflow: %s\nnext: %s\n", report.State, report.Next); err != nil {
		return err
	}
	for _, blocker := range report.Blockers {
		if _, err := fmt.Fprintf(writer, "? %s: %s\n", blocker.Type, blocker.ID); err != nil {
			return err
		}
	}
	return nil
}

func requireTerminalGraphEvidence(cwd, graphPath string, report pipeline.LifecycleReport) pipeline.LifecycleReport {
	if report.State != "complete" {
		return report
	}
	block := func(reason string) {
		report.Blockers = append(report.Blockers, pipeline.LifecycleBlocker{Stage: "evidence", Reason: reason})
	}
	graph, err := LoadGraph(cwd, graphPath)
	if err != nil {
		block(err.Error())
	} else {
		graphReport, evaluateErr := EvaluateGraph(graph)
		if evaluateErr != nil {
			block(evaluateErr.Error())
		} else {
			terminalFound := false
			for _, node := range graphReport.Nodes {
				if node.Kind == "terminal-lifecycle" {
					terminalFound = true
					if node.Status != "passed" {
						reason := fmt.Sprintf("terminal graph node %s is %s", node.ID, node.Status)
						if node.Reason != "" {
							reason += ": " + node.Reason
						}
						block(reason)
						continue
					}
				}
				switch node.Status {
				case "missing", "stale", "failed":
					reason := fmt.Sprintf("graph node %s is %s", node.ID, node.Status)
					if node.Reason != "" {
						reason += ": " + node.Reason
					}
					block(reason)
				}
			}
			if !terminalFound {
				block("graph has no terminal-lifecycle evidence node")
			}
		}
	}
	if len(report.Blockers) > 0 {
		report.State = "blocked"
		report.Next = "evidence"
	}
	return report
}

func declaredGraphPath(cwd, requested string) (string, bool) {
	if requested != "" {
		return requested, true
	}
	defaultPath := ".groundspec/graph.json"
	_, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(defaultPath)))
	return defaultPath, err == nil || !errors.Is(err, os.ErrNotExist)
}

func Execute(arguments []string, cwd string, stdout io.Writer) (int, error) {
	if len(arguments) == 0 {
		return 2, &UsageError{Message: usageText}
	}
	switch arguments[0] {
	case "help", "--help", "-h":
		_, err := fmt.Fprintln(stdout, usageText)
		return 0, err
	case "version", "--version":
		_, err := fmt.Fprintf(stdout, "groundspec %s\n", Version)
		return 0, err
	case "adapters":
		options, err := parseOptions(arguments[1:], map[string]optionKind{"json": booleanOption})
		if err != nil {
			return 2, err
		}
		descriptors := agent.Discover(context.Background())
		if options["json"] == "true" {
			if err := writeJSON(stdout, descriptors); err != nil {
				return 3, err
			}
		} else if len(descriptors) == 0 {
			_, err = fmt.Fprintln(stdout, "no compatible agent CLIs found")
		} else {
			for _, descriptor := range descriptors {
				state := "not ready"
				if descriptor.Ready {
					state = "ready"
				}
				if _, writeErr := fmt.Fprintf(stdout, "%s: %s (%s; %s)\n", descriptor.ID, state, descriptor.Version, descriptor.Auth); writeErr != nil {
					return 3, writeErr
				}
			}
		}
		if err != nil {
			return 3, err
		}
		return 0, nil
	case "start":
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return 2, &UsageError{Message: "start requires a source path"}
		}
		options, err := parseOptions(arguments[2:], map[string]optionKind{"output": valueOption, "adapter": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		if options["output"] == "" {
			return 2, &UsageError{Message: "start requires '--output directory'"}
		}
		selected, err := agent.Resolve(context.Background(), options["adapter"])
		if err != nil {
			return 2, invalidCause(err, "cannot select adapter: %v", err)
		}
		source := arguments[1]
		if !filepath.IsAbs(source) {
			source = filepath.Join(cwd, source)
		}
		output := options["output"]
		if !filepath.IsAbs(output) {
			output = filepath.Join(cwd, output)
		}
		workspace, err := pipeline.Start(context.Background(), source, output, selected)
		if err != nil {
			return 2, invalidCause(err, "cannot start workspace: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, workspace)
		} else {
			_, err = fmt.Fprintf(stdout, "workspace started: %s\nnext: review %s\n", workspace.Root, workspace.Proposal)
		}
		if err != nil {
			return 3, err
		}
		return 0, nil
	case "init":
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return 2, &UsageError{Message: "init requires a source path"}
		}
		options, err := parseOptions(arguments[2:], map[string]optionKind{"output": valueOption, "adapter": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		selected, err := agent.Resolve(context.Background(), options["adapter"])
		if err != nil {
			return 2, invalidCause(err, "cannot select adapter: %v", err)
		}
		source := arguments[1]
		if !filepath.IsAbs(source) {
			source = filepath.Join(cwd, source)
		}
		output := options["output"]
		if output == "" {
			output = cwd
		} else if !filepath.IsAbs(output) {
			output = filepath.Join(cwd, output)
		}
		workspace, err := pipeline.Init(context.Background(), source, output, selected)
		if err != nil {
			return 2, invalidCause(err, "cannot initialize project: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, workspace)
		} else {
			_, err = fmt.Fprintf(stdout, "project initialized: %s\nnext: review %s\n", workspace.Root, workspace.Proposal)
		}
		if err != nil {
			return 3, err
		}
		return 0, nil
	case "ingest":
		if len(arguments) < 2 || (len(arguments[1]) >= 2 && arguments[1][:2] == "--") {
			return 2, &UsageError{Message: "ingest requires a source path"}
		}
		sourcePath := arguments[1]
		options, err := parseOptions(arguments[2:], map[string]optionKind{"json": booleanOption})
		if err != nil {
			return 2, err
		}
		bundle, err := intake.Ingest(cwd, sourcePath)
		if err != nil {
			return 2, invalidCause(err, "cannot ingest source: %v", err)
		}
		if options["json"] == "true" {
			if err := writeJSON(stdout, bundle); err != nil {
				return 3, err
			}
		} else if _, err := fmt.Fprintf(stdout, "ingested %s: %d blocks\n", bundle.Source.Path, len(bundle.Blocks)); err != nil {
			return 3, err
		}
		return 0, nil
	case "draft":
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return 2, &UsageError{Message: "draft requires a source bundle path"}
		}
		options, err := parseOptions(arguments[2:], map[string]optionKind{"output": valueOption, "adapter": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		if options["output"] == "" {
			return 2, &UsageError{Message: "draft requires '--output proposal'"}
		}
		selected, err := agent.Resolve(context.Background(), options["adapter"])
		if err != nil {
			return 2, invalidCause(err, "cannot select adapter: %v", err)
		}
		proposal, err := pipeline.Draft(context.Background(), cwd, arguments[1], options["output"], selected)
		if err != nil {
			return 2, invalidCause(err, "cannot draft proposal: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, proposal)
		} else {
			_, err = fmt.Fprintf(stdout, "proposal drafted: %s\nnext: review\n", options["output"])
		}
		if err != nil {
			return 3, err
		}
		return 0, nil
	case "proposal":
		if len(arguments) < 3 || arguments[1] != "validate" || (len(arguments[2]) >= 2 && arguments[2][:2] == "--") {
			return 2, &UsageError{Message: "proposal requires 'validate <proposal>'"}
		}
		proposalPath := arguments[2]
		options, err := parseOptions(arguments[3:], map[string]optionKind{"json": booleanOption})
		if err != nil {
			return 2, err
		}
		validated, err := workflow.ValidateProposal(cwd, proposalPath)
		if err != nil {
			return 2, invalidCause(err, "cannot validate proposal: %v", err)
		}
		if options["json"] == "true" {
			if err := writeJSON(stdout, validated.Report); err != nil {
				return 3, err
			}
		} else if _, err := fmt.Fprintf(stdout, "proposal valid: %d requirements, %d questions\n", validated.Report.Requirements, validated.Report.Questions); err != nil {
			return 3, err
		}
		return 0, nil
	case "review":
		if len(arguments) < 2 || (len(arguments[1]) >= 2 && arguments[1][:2] == "--") {
			return 2, &UsageError{Message: "review requires a proposal path"}
		}
		proposalPath := arguments[1]
		options, err := parseOptions(arguments[2:], map[string]optionKind{
			"review":  valueOption,
			"accept":  valueOption,
			"reject":  valueOption,
			"resolve": valueOption,
			"answer":  valueOption,
			"note":    valueOption,
			"json":    booleanOption,
		})
		if err != nil {
			return 2, err
		}
		actionCount := 0
		for _, name := range []string{"accept", "reject", "resolve"} {
			if _, exists := options[name]; exists {
				actionCount++
			}
		}
		if actionCount != 1 {
			return 2, &UsageError{Message: "review requires exactly one of '--accept id', '--reject id', or '--resolve id --answer text'"}
		}
		action := workflow.ReviewAction{}
		acceptID, hasAccept := options["accept"]
		rejectID, hasReject := options["reject"]
		switch {
		case hasAccept:
			action = workflow.ReviewAction{Kind: "accept", ID: acceptID, Text: options["note"]}
		case hasReject:
			action = workflow.ReviewAction{Kind: "reject", ID: rejectID, Text: options["note"]}
		default:
			if _, exists := options["answer"]; !exists {
				return 2, &UsageError{Message: "review '--resolve id' requires '--answer text'"}
			}
			if _, exists := options["note"]; exists {
				return 2, &UsageError{Message: "review '--note' is only valid with '--accept' or '--reject'"}
			}
			action = workflow.ReviewAction{Kind: "resolve", ID: options["resolve"], Text: options["answer"]}
		}
		if _, exists := options["answer"]; exists && action.Kind != "resolve" {
			return 2, &UsageError{Message: "review '--answer' is only valid with '--resolve'"}
		}
		reviewPath := options["review"]
		if reviewPath == "" {
			reviewPath = defaultReviewPath
		}
		review, err := workflow.ApplyReview(cwd, proposalPath, reviewPath, action)
		if err != nil {
			return 2, invalidCause(err, "cannot record review: %v", err)
		}
		if options["json"] == "true" {
			if err := writeJSON(stdout, review); err != nil {
				return 3, err
			}
		} else {
			result := action.Kind
			if action.Kind == "accept" {
				result = "accepted"
			} else if action.Kind == "reject" {
				result = "rejected"
			} else if action.Kind == "resolve" {
				result = "resolved"
			}
			if _, err := fmt.Fprintf(stdout, "reviewed %s: %s\n", action.ID, result); err != nil {
				return 3, err
			}
		}
		return 0, nil
	case "status":
		if len(arguments) < 2 || (len(arguments[1]) >= 2 && arguments[1][:2] == "--") {
			return 2, &UsageError{Message: "status requires a proposal path"}
		}
		proposalPath := arguments[1]
		options, err := parseOptions(arguments[2:], map[string]optionKind{
			"review": valueOption,
			"json":   booleanOption,
		})
		if err != nil {
			return 2, err
		}
		reviewPath := options["review"]
		if reviewPath == "" {
			reviewPath = defaultReviewPath
		}
		report, err := workflow.Status(cwd, proposalPath, reviewPath)
		if err != nil {
			return 2, invalidCause(err, "cannot report workflow status: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, report)
		} else {
			err = writeWorkflowStatus(stdout, report)
		}
		if err != nil {
			return 3, err
		}
		if report.State == "ready" {
			return 0, nil
		}
		return 1, nil
	case "materialize":
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return 2, &UsageError{Message: "materialize requires a proposal path"}
		}
		options, err := parseOptions(arguments[2:], map[string]optionKind{"review": valueOption, "output": valueOption, "adapter": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		reviewPath := options["review"]
		if reviewPath == "" {
			reviewPath = defaultReviewPath
		}
		outputPath := options["output"]
		if outputPath == "" {
			outputPath = ".groundspec/development-plan.json"
		}
		selected, err := agent.Resolve(context.Background(), options["adapter"])
		if err != nil {
			return 2, invalidCause(err, "cannot select adapter: %v", err)
		}
		plan, err := pipeline.Materialize(context.Background(), cwd, arguments[1], reviewPath, outputPath, selected)
		if err != nil {
			return 2, invalidCause(err, "cannot materialize plan: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, plan)
		} else {
			_, err = fmt.Fprintf(stdout, "specification set materialized from %d reviewed requirements\nnext: implement %s\n", len(plan.Requirements), outputPath)
		}
		if err != nil {
			return 3, err
		}
		return 0, nil
	case "implement":
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return 2, &UsageError{Message: "implement requires a development plan path"}
		}
		options, err := parseOptions(arguments[2:], map[string]optionKind{"output": valueOption, "adapter": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		outputPath := options["output"]
		if outputPath == "" {
			outputPath = ".groundspec/implementation-result.json"
		}
		selected, err := agent.Resolve(context.Background(), options["adapter"])
		if err != nil {
			return 2, invalidCause(err, "cannot select adapter: %v", err)
		}
		result, err := pipeline.Implement(context.Background(), cwd, arguments[1], outputPath, selected)
		if err != nil {
			return 2, invalidCause(err, "cannot implement plan: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, result)
		} else {
			_, err = fmt.Fprintf(stdout, "implementation recorded: %d artifacts\nnext: verify %s\n", len(result.Artifacts), arguments[1])
		}
		if err != nil {
			return 3, err
		}
		return 0, nil
	case "verify":
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return 2, &UsageError{Message: "verify requires a development plan path"}
		}
		options, err := parseOptions(arguments[2:], map[string]optionKind{"implementation": valueOption, "output": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		implementationPath := options["implementation"]
		if implementationPath == "" {
			implementationPath = ".groundspec/implementation-result.json"
		}
		outputPath := options["output"]
		if outputPath == "" {
			outputPath = ".groundspec/verification.json"
		}
		result, err := pipeline.Verify(context.Background(), cwd, arguments[1], implementationPath, outputPath)
		if err != nil {
			return 2, invalidCause(err, "cannot verify implementation: %v", err)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, result)
		} else {
			_, err = fmt.Fprintf(stdout, "verification passed: %t (%d commands)\nnext: lifecycle\n", result.Passed, len(result.Commands))
		}
		if err != nil {
			return 3, err
		}
		if result.Passed {
			return 0, nil
		}
		return 1, nil
	case "lifecycle":
		options, err := parseOptions(arguments[1:], map[string]optionKind{"proposal": valueOption, "review": valueOption, "plan": valueOption, "implementation": valueOption, "verification": valueOption, "graph": valueOption, "json": booleanOption})
		if err != nil {
			return 2, err
		}
		proposalPath := options["proposal"]
		if proposalPath == "" {
			proposalPath = ".groundspec/proposal.json"
		}
		reviewPath := options["review"]
		if reviewPath == "" {
			reviewPath = defaultReviewPath
		}
		planPath := options["plan"]
		if planPath == "" {
			planPath = ".groundspec/development-plan.json"
		}
		implementationPath := options["implementation"]
		if implementationPath == "" {
			implementationPath = ".groundspec/implementation-result.json"
		}
		verificationPath := options["verification"]
		if verificationPath == "" {
			verificationPath = ".groundspec/verification.json"
		}
		report := pipeline.Lifecycle(cwd, proposalPath, reviewPath, planPath, implementationPath, verificationPath)
		if graphPath, required := declaredGraphPath(cwd, options["graph"]); required {
			report = requireTerminalGraphEvidence(cwd, graphPath, report)
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, report)
		} else {
			_, err = fmt.Fprintf(stdout, "groundspec lifecycle: %s\nnext: %s\n", report.State, report.Next)
			for _, blocker := range report.Blockers {
				if _, writeErr := fmt.Fprintf(stdout, "? %s: %s\n", blocker.Stage, blocker.Reason); writeErr != nil {
					return 3, writeErr
				}
			}
		}
		if err != nil {
			return 3, err
		}
		if report.State == "complete" {
			return 0, nil
		}
		return 1, nil
	case "check":
		options, err := parseOptions(arguments[1:], map[string]optionKind{
			"graph": valueOption,
			"json":  booleanOption,
		})
		if err != nil {
			return 2, err
		}
		graphPath := options["graph"]
		if graphPath == "" {
			graphPath = ".groundspec/graph.json"
		}
		graph, err := LoadGraph(cwd, graphPath)
		if err != nil {
			return 2, err
		}
		report, err := EvaluateGraph(graph)
		if err != nil {
			return 2, err
		}
		if options["json"] == "true" {
			err = writeJSON(stdout, report)
		} else {
			err = writeHumanReport(stdout, report)
		}
		if err != nil {
			return 3, err
		}
		if report.Overall == "healthy" {
			return 0, nil
		}
		return 1, nil
	case "attest":
		if len(arguments) < 2 || (len(arguments[1]) >= 2 && arguments[1][:2] == "--") {
			return 2, &UsageError{Message: "attest requires a node id"}
		}
		nodeID := arguments[1]
		options, err := parseOptions(arguments[2:], map[string]optionKind{
			"graph":    valueOption,
			"result":   valueOption,
			"evidence": valueOption,
			"note":     valueOption,
			"json":     booleanOption,
		})
		if err != nil {
			return 2, err
		}
		if options["result"] == "" {
			return 2, &UsageError{Message: "attest requires '--result passed|failed'"}
		}
		graphPath := options["graph"]
		if graphPath == "" {
			graphPath = ".groundspec/graph.json"
		}
		graph, err := LoadGraph(cwd, graphPath)
		if err != nil {
			return 2, err
		}
		var note *string
		if value, exists := options["note"]; exists {
			note = &value
		}
		var evidence *string
		if value, exists := options["evidence"]; exists {
			evidence = &value
		}
		proof, err := AttestNode(graph, nodeID, AttestOptions{
			Result:   options["result"],
			Evidence: evidence,
			Note:     note,
		})
		if err != nil {
			return 2, err
		}
		if options["json"] == "true" {
			if err := writeJSON(stdout, proof); err != nil {
				return 3, err
			}
		} else if _, err := fmt.Fprintf(stdout, "attested %s: %s\n", proof.Node, proof.Result); err != nil {
			return 3, err
		}
		return 0, nil
	default:
		return 2, &UsageError{Message: usageText}
	}
}

func IsExpectedError(err error) bool {
	var validation *ValidationError
	var usage *UsageError
	return errors.As(err, &validation) || errors.As(err, &usage)
}
