package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"
	"gopkg.in/yaml.v3"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/eval"
	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/hook/agy"
	"user-prompt-checker/internal/hook/codex"
	"user-prompt-checker/internal/installer"
	"user-prompt-checker/internal/tui"
)

type cli struct {
	Model     string   `help:"Ollama model name." default:"d1:2b"`
	URL       string   `help:"Ollama base URL." default:"http://localhost:11434"`
	Rubric    string   `help:"Path to custom rubric YAML or JSON (defaults to embedded rubric)."`
	Threshold float64  `help:"Minimum score threshold in [0, 1]." default:"0.6"`
	Timeout   *float64 `help:"HTTP timeout in seconds (default: 120 for check/evaluate, 10 for hook)."`

	Check     checkCmd     `cmd:"" help:"Check a single user prompt."`
	Evaluate  evaluateCmd  `cmd:"" help:"Run all evaluation cases and save report."`
	Hook      hookCmd      `cmd:"" help:"Run an agent lifecycle hook (codex or agy)."`
	Install   installCmd   `cmd:"" help:"Register user-prompt-checker into agent hooks.json."`
	Uninstall uninstallCmd `cmd:"" help:"Remove user-prompt-checker from agent hooks.json."`
	Gain      gainCmd      `cmd:"" help:"Analyze prompt score improvements and quality history."`
}

type checkCmd struct {
	Prompt  string `arg:"" help:"Prompt to check (use - for stdin)."`
	History string `help:"Path to conversation history JSON array."`
}

type evaluateCmd struct {
	Cases  string `help:"Path to evaluation cases YAML or JSON." default:"cases.yaml"`
	Output string `help:"Path to write results JSON." default:"results.json"`
}

type hookCmd struct {
	Agent string `arg:"" enum:"codex,agy" help:"Target agent platform (codex or agy)."`
}

type gainCmd struct {
	Dir    string `help:"Path to history directory (defaults to XDG data dir)."`
	Format string `help:"Output format: tui, text, or json (default: auto-detected tui on TTY, text on pipe)." default:""`
}

type installCmd struct {
	Agent  string `arg:"" enum:"agy,codex" help:"Target agent platform (agy or codex)."`
	Binary string `help:"Path to executable binary (default: auto-detected current binary)."`
	Target string `help:"Target hooks.json path (default: agent standard location)."`
}

type uninstallCmd struct {
	Agent  string `arg:"" enum:"agy,codex" help:"Target agent platform (agy or codex)."`
	Target string `help:"Target hooks.json path (default: agent standard location)."`
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
			parsed.Hook.writeError(stdout, errors.New("invalid threshold or timeout"))
			return 0
		}
		fmt.Fprintln(stderr, "Error: threshold must be in [0, 1] and timeout must be a positive finite number")
		return 2
	}

	rubric, err := checker.LoadRubric(parsed.Rubric)
	if err != nil {
		if isHook {
			parsed.Hook.writeError(stdout, err)
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
			if err := yaml.Unmarshal(b, &history); err != nil {
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

	case "install <agent>":
		binPath := parsed.Install.Binary
		if binPath == "" {
			var err error
			binPath, err = os.Executable()
			if err != nil {
				binPath = "user-prompt-checker"
			}
		}
		if err := installer.Install(parsed.Install.Agent, binPath, parsed.Install.Target); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		target := parsed.Install.Target
		if target == "" {
			paths, _ := installer.DefaultConfigPaths()
			if parsed.Install.Agent == "agy" {
				target = paths.AGY
			} else {
				target = paths.Codex
			}
		}
		fmt.Fprintf(stdout, "Successfully installed hook for %s to %s\n", parsed.Install.Agent, target)
		return 0

	case "uninstall <agent>":
		if err := installer.Uninstall(parsed.Uninstall.Agent, parsed.Uninstall.Target); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		target := parsed.Uninstall.Target
		if target == "" {
			paths, _ := installer.DefaultConfigPaths()
			if parsed.Uninstall.Agent == "agy" {
				target = paths.AGY
			} else {
				target = paths.Codex
			}
		}
		fmt.Fprintf(stdout, "Successfully uninstalled hook for %s from %s\n", parsed.Uninstall.Agent, target)
		return 0

	case "gain":
		records, err := history.LoadAll(parsed.Gain.Dir)
		if err != nil {
			fmt.Fprintf(stderr, "Error loading history: %v\n", err)
			return 1
		}
		summary := history.Analyze(records)

		format := parsed.Gain.Format
		if format == "tui" || (format == "" && isTerminalWriter(stdout)) {
			if err := tui.Run(summary); err != nil {
				fmt.Fprintf(stderr, "Error running TUI: %v\n", err)
				return 1
			}
			return 0
		}

		if format == "json" {
			out, _ := json.MarshalIndent(summary, "", "  ")
			fmt.Fprintln(stdout, string(out))
			return 0
		}
		history.RenderGainReport(summary, stdout)
		return 0

	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", cmd)
		return 2
	}
}

type kongExit struct{}

func (cmd hookCmd) writeError(stdout io.Writer, err error) {
	switch cmd.Agent {
	case "agy":
		agy.WriteError(stdout, err)
	case "codex":
		codex.WriteError(stdout, err)
	}
}

func isTerminalWriter(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
