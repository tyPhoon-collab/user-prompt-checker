package eval

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"user-prompt-checker/internal/checker"
)

func TestHTMLReportShowsMismatchesAndEscapesInput(t *testing.T) {
	rubric, err := checker.LoadRubric("")
	if err != nil {
		t.Fatal(err)
	}
	rows := []Row{
		{Case: "1: false block", Expected: map[string]bool{"intent_clear": true, "context_sufficient": true, "consistent": true}, Result: checker.Result{
			Prompt: `<script>alert("prompt")</script>`, History: []checker.Message{{Role: "user", Content: "<img src=x onerror=alert(1)>"}},
			Scores: map[string]float64{"intent_clear": 0.2, "context_sufficient": 0.9, "consistent": 0.9}, Clarifications: []string{"<b>question</b>"},
		}},
		{Case: "2: false pass", Expected: map[string]bool{"intent_clear": false, "context_sufficient": true, "consistent": true}, Result: checker.Result{
			Pass: true, Scores: map[string]float64{"intent_clear": 0.6, "context_sufficient": 0.9, "consistent": 0.9},
		}},
		{Case: "3: aspect mismatch", Expected: map[string]bool{"intent_clear": false, "context_sufficient": true, "consistent": true}, Result: checker.Result{
			Scores: map[string]float64{"intent_clear": 0.6, "context_sufficient": 0.2, "consistent": 0.9},
		}},
		{Case: "4: match", Expected: map[string]bool{"intent_clear": true, "context_sufficient": true, "consistent": true}, Result: checker.Result{
			Pass: true, Scores: map[string]float64{"intent_clear": 0.6, "context_sufficient": 0.9, "consistent": 0.9},
		}},
	}
	data, err := renderHTML(report{Model: "test-model", Rubric: rubric, Results: rows, Summary: Summarize(rows, rubric, 0.6)})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{"不一致のみ（3件）", "誤ブロック", "見逃し", "判定一致・観点不一致", "&lt;script&gt;", "&lt;img", "&lt;b&gt;question&lt;/b&gt;", `data-mismatch="false" hidden`, "0.600"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML does not contain %q", want)
		}
	}
	for _, unsafe := range []string{`<script>alert`, `<img src=x`, `<b>question</b>`, "ZgotmplZ"} {
		if strings.Contains(html, unsafe) {
			t.Errorf("HTML contains unsafe or invalid content %q", unsafe)
		}
	}
}

func TestRunWritesSiblingHTML(t *testing.T) {
	rubric, err := checker.LoadRubric("")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ output, html string }{
		{"custom.json", "custom.html"},
		{"custom", "custom.html"},
		{"custom.html", "custom.html.html"},
	} {
		t.Run(tc.output, func(t *testing.T) {
			dir := t.TempDir()
			casesPath := filepath.Join(dir, "cases.yaml")
			if err := os.WriteFile(casesPath, []byte("- prompt: /test\n  history:\n    - role: user\n      content: 以前の依頼\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Run(casesPath, filepath.Join(dir, tc.output), rubric, checker.Config{Threshold: 0.6}, io.Discard, nil); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, tc.html))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"<pre>/test</pre>", "以前の依頼", "期待値と異なるケースはない。"} {
				if !strings.Contains(string(data), want) {
					t.Errorf("HTML does not contain %q", want)
				}
			}
			if err := os.Mkdir(filepath.Join(dir, "blocked.html"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := Run(casesPath, filepath.Join(dir, "blocked.json"), rubric, checker.Config{Threshold: 0.6}, io.Discard, nil); err == nil {
				t.Fatal("expected error when HTML cannot be written")
			}
		})
	}
}
