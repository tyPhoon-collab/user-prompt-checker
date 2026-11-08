package main

import (
	"bytes"
	"strings"
	"testing"
)

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
}
