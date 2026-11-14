package checker

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func mustLoadRubric(t *testing.T) Rubric {
	t.Helper()
	r, err := LoadRubric("")
	if err != nil {
		t.Fatalf("LoadRubric failed: %v", err)
	}
	return r
}

func TestContextAndPromptAreSeparateAndAllQuestionsAreSent(t *testing.T) {
	rubric := mustLoadRubric(t)
	history := []Message{{Role: "assistant", Content: "Proceed with option B?"}}
	payload, err := MakePayload("Yes", history, rubric, "d1:2b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantState := map[string]any{"conversation": history, "user_prompt": "Yes"}
	if diff := cmp.Diff(wantState, payload["state"]); diff != "" {
		t.Fatalf("state mismatch (-want +got):\n%s", diff)
	}
	questions := payload["questions"].(map[string]any)
	if len(questions) != len(rubric) {
		t.Fatalf("expected %d questions, got %d", len(rubric), len(questions))
	}
	for k, v := range questions {
		q := v.(map[string]any)
		if q["type"] != "noul" {
			t.Fatalf("question %s has wrong type: %v", k, q["type"])
		}
	}
}

func TestInvalidInputIsRejectedBeforeNetworkCall(t *testing.T) {
	rubric := mustLoadRubric(t)
	called := false
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("should not be called")
		}),
	}
	cfg := Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	if _, err := Check("   ", nil, rubric, cfg, client); err == nil {
		t.Fatal("expected error for blank prompt")
	}
	if _, err := Check("Proceed", []Message{{Role: "system", Content: "x"}}, rubric, cfg, client); err == nil {
		t.Fatal("expected error for invalid history role")
	}
	if called {
		t.Fatal("network should not have been called")
	}
}

func TestEmptyRubricCannotSilentlyPass(t *testing.T) {
	if _, err := MakePayload("Change it", nil, Rubric{}, "d1:2b"); err == nil {
		t.Fatal("expected error for empty rubric")
	}
}

func TestMissingOrInvalidModelScoresAreErrors(t *testing.T) {
	rubric := mustLoadRubric(t)
	for _, val := range []any{nil, true, "0.9", -0.1, 1.1} {
		raw := map[string]any{
			"answers": map[string]any{
				"intent_clear":       map[string]any{"type": "noul", "noul": val},
				"context_sufficient": map[string]any{"type": "noul", "noul": 0.8},
				"consistent":         map[string]any{"type": "noul", "noul": 0.8},
			},
		}
		if _, err := ExtractScores(raw, rubric); err == nil {
			t.Fatalf("expected error for invalid score %v", val)
		}
	}
	if _, err := ExtractScores(map[string]any{"answers": map[string]any{}}, rubric); err == nil {
		t.Fatal("expected error for empty answers")
	}
}

func TestOneLowAspectBlocksAndBoundaryPasses(t *testing.T) {
	rubric := mustLoadRubric(t)
	scores1 := map[string]float64{"intent_clear": 0.6, "context_sufficient": 0.5999, "consistent": 0.9}
	if diff := cmp.Diff([]string{"context_sufficient"}, Decide(scores1, rubric, 0.6)); diff != "" {
		t.Fatalf("failed aspects mismatch (-want +got):\n%s", diff)
	}
	scores2 := map[string]float64{"intent_clear": 0.6, "context_sufficient": 0.6, "consistent": 1.0}
	if diff := cmp.Diff([]string{}, Decide(scores2, rubric, 0.6)); diff != "" {
		t.Fatalf("boundary pass mismatch (-want +got):\n%s", diff)
	}
}

func TestIsCommandOrSkillOnly(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		{"/test", true},
		{"$compact", true},
		{"/commit", true},
		{"$compact $before-after", true},
		{"/test /review", true},
		{"  $den-show-me   /help  ", true},
		{"/plugin:skill-name", true},
		{"$plugin:skill-name", true},
		{"/test これ実行して", false},
		{"$compact 要約して", false},
		{"/etc/hosts", false},
		{"/:", false},
		{"/:foo", false},
		{"/foo:", false},
		{"普通のプロンプト", false},
		{"", false},
		{"   ", false},
		{"$", false},
		{"/", false},
	}
	for _, tc := range tests {
		got := IsCommandOrSkillOnly(tc.prompt)
		if got != tc.want {
			t.Errorf("IsCommandOrSkillOnly(%q) = %v; want %v", tc.prompt, got, tc.want)
		}
	}
}

func TestCommandOrSkillBypassesModelCall(t *testing.T) {
	rubric := mustLoadRubric(t)
	cfg := Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	// client is nil or will fail if called
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP request should not be made for command/skill invocations")
			return nil, nil
		}),
	}

	res, err := Check("$compact $before-after", nil, rubric, cfg, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Pass {
		t.Fatal("expected pass, got block")
	}
	for k, score := range res.Scores {
		if score != 1.0 {
			t.Errorf("expected score 1.0 for %s, got %v", k, score)
		}
	}
}

func TestLoadRubricSupportsJSONAndRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "rubric.json")
	validJSON := `{
		"intent_clear": {"label": "a", "question": "q", "criteria": {"true": "t", "false": "f"}, "clarification": "c"},
		"context_sufficient": {"label": "a", "question": "q", "criteria": {"true": "t", "false": "f"}, "clarification": "c"},
		"consistent": {"label": "a", "question": "q", "criteria": {"true": "t", "false": "f"}, "clarification": "c"}
	}`
	if err := os.WriteFile(jsonPath, []byte(validJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRubric(jsonPath); err != nil {
		t.Fatalf("expected valid JSON rubric to load, got: %v", err)
	}

	typoPath := filepath.Join(dir, "typo.yaml")
	typoYAML := "intent_clear:\n  labl: typo\n  question: q\n  criteria:\n    \"true\": t\n    \"false\": f\n  clarification: c\n"
	if err := os.WriteFile(typoPath, []byte(typoYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRubric(typoPath); err == nil {
		t.Fatal("expected error for unknown field in YAML rubric")
	}
}
