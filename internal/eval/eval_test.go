package eval

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"user-prompt-checker/internal/checker"
)

func TestCaseLabel(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   string
	}{
		{"\n  それで\n進めて。\t", "3: それで 進めて。"},
		{strings.Repeat("あ", 30), "3: " + strings.Repeat("あ", 30)},
		{strings.Repeat("あ", 31), "3: " + strings.Repeat("あ", 30) + "…"},
	} {
		if got := caseLabel(3, tc.prompt); got != tc.want {
			t.Errorf("caseLabel(3, %q) = %q; want %q", tc.prompt, got, tc.want)
		}
	}
}

func TestRunDistinguishesIdenticalPrompts(t *testing.T) {
	rubric, err := checker.LoadRubric("")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.yaml")
	outputPath := filepath.Join(dir, "results.json")
	if err := os.WriteFile(casesPath, []byte("- prompt: /test\n- prompt: /test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Run(casesPath, outputPath, rubric, checker.Config{Threshold: 0.6}, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"1: /test", "2: /test"} {
		if !strings.Contains(stdout.String(), label) || !strings.Contains(string(data), `"case": "`+label+`"`) {
			t.Errorf("missing label %q in terminal output or report", label)
		}
	}
}

func TestRunExpectedDefaults(t *testing.T) {
	rubric, err := checker.LoadRubric("")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		expected string
		want     map[string]bool
		wantErr  string
	}{
		{"omitted", "", map[string]bool{"intent_clear": true, "context_sufficient": true}, ""},
		{"empty", "  expected: {}\n", map[string]bool{"intent_clear": true, "context_sufficient": true}, ""},
		{"partial", "  expected:\n    context_sufficient: false\n", map[string]bool{"intent_clear": true, "context_sufficient": false}, ""},
		{"explicit", "  expected:\n    intent_clear: false\n    context_sufficient: true\n", map[string]bool{"intent_clear": false, "context_sufficient": true}, ""},
		{"unknown", "  expected:\n    intent_cler: false\n", nil, "unknown expected key intent_cler in case 1: /test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			casesPath := filepath.Join(dir, "cases.yaml")
			outputPath := filepath.Join(dir, "results.json")
			if err := os.WriteFile(casesPath, []byte("- prompt: /test\n"+tc.expected), 0o600); err != nil {
				t.Fatal(err)
			}
			err := Run(casesPath, outputPath, rubric, checker.Config{Threshold: 0.6}, io.Discard, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Run error = %v; want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			var report struct {
				Results []Row   `json:"results"`
				Summary Summary `json:"summary"`
			}
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Results) != 1 {
				t.Fatalf("got %d results; want 1", len(report.Results))
			}
			tc.want["consistent"] = true
			if diff := cmp.Diff(tc.want, report.Results[0].Expected); diff != "" {
				t.Fatalf("expected mismatch (-want +got):\n%s", diff)
			}
			wantMatches := 0
			if tc.want["intent_clear"] && tc.want["context_sufficient"] {
				wantMatches = 1
			}
			if report.Summary.GateMatches != wantMatches {
				t.Fatalf("gate matches = %d; want %d", report.Summary.GateMatches, wantMatches)
			}
		})
	}
}

func TestSummaryDistinguishesFalseBlocksAndFalsePasses(t *testing.T) {
	rubric := checker.Rubric{"intent_clear": {}}
	rows := []Row{
		{
			Case:     "1: good",
			Expected: map[string]bool{"intent_clear": true},
			Result: checker.Result{
				Pass:      false,
				Scores:    map[string]float64{"intent_clear": 0.2},
				ElapsedMS: 10,
			},
		},
		{
			Case:     "2: bad",
			Expected: map[string]bool{"intent_clear": false},
			Result: checker.Result{
				Pass:      true,
				Scores:    map[string]float64{"intent_clear": 0.9},
				ElapsedMS: 20,
			},
		},
	}
	want := Summary{
		Count:         2,
		Threshold:     0.6,
		GateMatches:   0,
		FalseBlocks:   []string{"1: good"},
		FalsePasses:   []string{"2: bad"},
		AspectMatches: map[string]int{"intent_clear": 0},
		LatencyMS:     map[string]float64{"first": 10, "median": 15, "max": 20},
	}
	if diff := cmp.Diff(want, Summarize(rows, rubric, 0.6)); diff != "" {
		t.Fatalf("summary mismatch (-want +got):\n%s", diff)
	}
}
