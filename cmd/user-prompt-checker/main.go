package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/eval"
	"user-prompt-checker/internal/hook/agy"
	"user-prompt-checker/internal/hook/codex"
)

type cli struct {
	Model     string   `help:"Ollama model name." default:"d1:2b"`
	URL       string   `help:"Ollama base URL." default:"http://localhost:11434"`
	Rubric    string   `help:"Path to custom rubric.json (defaults to embedded rubric)."`
	Threshold float64  `help:"Minimum score threshold in [0, 1]." default:"0.6"`
	Timeout   *float64 `help:"HTTP timeout in seconds (default: 120 for check/evaluate, 10 for hook)."`

	Check    checkCmd    `cmd:"" help:"Check a single user prompt."`
	Evaluate evaluateCmd `cmd:"" help:"Run all evaluation cases and save report."`
	Hook     hookCmd     `cmd:"" help:"Run an agent lifecycle hook (codex or agy)."`
}

type checkCmd struct {
	Prompt  string `arg:"" help:"Prompt to check (use - for stdin)."`
	History string `help:"Path to conversation history JSON array."`
}

type evaluateCmd struct {
	Cases  string `help:"Path to evaluation cases JSON." default:"cases.json"`
	Output string `help:"Path to write results JSON." default:"results.json"`
}

type hookCmd struct {
	Agent string `arg:"" enum:"codex,agy" help:"Target agent platform (codex or agy)."`
}

func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int) {
	var parsed cli
	parser, err := kong.New(
		&parsed,
		kong.Name("user-prompt-checker"),
		kong.Description("Check user prompts against evaluation criteria using Ollama d1:2b."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) {
			exitCode = code
			panic(kongExit{})
		}),
	)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(kongExit); !ok {
				panic(r)
			}
		}
	}()

	ctx, err := parser.Parse(args)
	if err != nil {
		parser.FatalIfErrorf(err)
		return 2
	}

	cmd := ctx.Command()
	isHook := strings.HasPrefix(cmd, "hook")
	timeoutSec := 120.0
	if isHook {
		timeoutSec = 10.0
	}
	if parsed.Timeout != nil {
		timeoutSec = *parsed.Timeout
	}

	if math.IsNaN(parsed.Threshold) || parsed.Threshold < 0 || parsed.Threshold > 1 || math.IsNaN(timeoutSec) || math.IsInf(timeoutSec, 0) || timeoutSec <= 0 {
		if isHook {
			if parsed.Hook.Agent == "agy" {
				resp, _ := json.Marshal(map[string]any{
					"terminationBehavior": "terminate",
					"injectSteps": []map[string]any{
						{"errorMessage": map[string]any{"userMessage": "Prompt check failed: invalid threshold or timeout"}},
					},
				})
				fmt.Fprintln(stdout, string(resp))
				return 0
			}
			resp, _ := json.Marshal(map[string]any{"decision": "block", "reason": "Prompt check failed: invalid threshold or timeout"})
			fmt.Fprintln(stdout, string(resp))
			return 0
		}
		fmt.Fprintln(stderr, "Error: threshold must be in [0, 1] and timeout must be a positive finite number")
		return 2
	}

	rubric, err := checker.LoadRubric(parsed.Rubric)
	if err != nil {
		if isHook {
			if parsed.Hook.Agent == "agy" {
				resp, _ := json.Marshal(map[string]any{
					"terminationBehavior": "terminate",
					"injectSteps": []map[string]any{
						{"errorMessage": map[string]any{"userMessage": "Prompt check failed: " + err.Error()}},
					},
				})
				fmt.Fprintln(stdout, string(resp))
				return 0
			}
			resp, _ := json.Marshal(map[string]any{"decision": "block", "reason": "Prompt check failed: " + err.Error()})
			fmt.Fprintln(stdout, string(resp))
			return 0
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	cfg := checker.Config{
		Model:     parsed.Model,
		URL:       parsed.URL,
		Threshold: parsed.Threshold,
		Timeout:   time.Duration(timeoutSec * float64(time.Second)),
	}

	if isHook {
		switch parsed.Hook.Agent {
		case "agy":
			agy.Run(stdin, stdout, rubric, cfg, nil)
			return 0
		case "codex":
			codex.Run(stdin, stdout, rubric, cfg, nil)
			return 0
		default:
			fmt.Fprintf(stderr, "Unknown hook agent: %s\n", parsed.Hook.Agent)
			return 2
		}
	}

	switch cmd {
	case "check <prompt>":
		prompt := parsed.Check.Prompt
		if prompt == "-" {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			prompt = string(b)
		}
		history := []checker.Message{}
		if parsed.Check.History != "" {
			b, err := os.ReadFile(parsed.Check.History)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			if err := json.Unmarshal(b, &history); err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
		}
		res, err := checker.Check(prompt, history, rubric, cfg, nil)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return 0

	case "evaluate":
		if err := eval.Run(parsed.Evaluate.Cases, parsed.Evaluate.Output, rubric, cfg, stdout, nil); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		return 0

	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", cmd)
		return 2
	}
}

type kongExit struct{}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
