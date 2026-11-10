package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestHookConfigurationErrors(t *testing.T) {
	rubricPath := filepath.Join(t.TempDir(), "rubric.json")
	if err := os.WriteFile(rubricPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"codex", "agy"} {
		for _, tc := range []struct {
			name   string
			flags  []string
			reason string
		}{
			{"invalid threshold", []string{"--threshold=1.5"}, "invalid threshold or timeout"},
			{"invalid rubric", []string{"--rubric", rubricPath}, "rubric must define exactly 3 criteria"},
		} {
			t.Run(agent+"/"+tc.name, func(t *testing.T) {
				var out, errBuf bytes.Buffer
				args := append(append([]string{}, tc.flags...), "hook", agent)
				code := runCLI(args, strings.NewReader("{}"), &out, &errBuf)
				if code != 0 || errBuf.Len() != 0 {
					t.Fatalf("expected exit code 0 and empty stderr, got %d and %q", code, errBuf.String())
				}
				reason := "Prompt check failed: " + tc.reason
				want := map[string]any{"decision": "block", "reason": reason}
				if agent == "agy" {
					want = map[string]any{
						"terminationBehavior": "terminate",
						"injectSteps": []any{
							map[string]any{"errorMessage": map[string]any{"userMessage": reason}},
						},
					}
				}
				var got map[string]any
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatalf("invalid hook response: %v (stdout: %q)", err, out.String())
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestCLISubcommandAndFlagParsing(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runCLI([]string{"--help"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0 for --help, got %d (stderr: %s)", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "check <prompt>") {
		t.Fatalf("expected check <prompt> in help output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "hook") {
		t.Fatalf("expected hook in help output:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = runCLI([]string{"--threshold=1.5", "check", "test"}, strings.NewReader(""), &out, &errBuf)
	if code != 2 {
		t.Fatalf("expected exit code 2 for out-of-range threshold, got %d", code)
	}

	out.Reset()
	errBuf.Reset()
	code = runCLI([]string{"hook"}, strings.NewReader("{}"), &out, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit code when hook agent is missing, got 0")
	}

	out.Reset()
	errBuf.Reset()
	code = runCLI([]string{"hook", "unknown-agent"}, strings.NewReader("{}"), &out, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit code for unknown agent, got 0")
	}

	out.Reset()
	errBuf.Reset()
	code = runCLI([]string{"install", "--help"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0 for install --help, got %d", code)
	}
	if !strings.Contains(out.String(), "install <agent>") {
		t.Fatalf("expected install <agent> in help output:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = runCLI([]string{"uninstall", "--help"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0 for uninstall --help, got %d", code)
	}
	if !strings.Contains(out.String(), "uninstall <agent>") {
		t.Fatalf("expected uninstall <agent> in help output:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = runCLI([]string{"gain", "--help"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0 for gain --help, got %d", code)
	}
	if !strings.Contains(out.String(), "gain") {
		t.Fatalf("expected gain in help output:\n%s", out.String())
	}
}
