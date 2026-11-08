package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"user-prompt-checker/internal/checker"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func mustLoadRubric(t *testing.T) checker.Rubric {
	t.Helper()
	r, err := checker.LoadRubric("")
	if err != nil {
		t.Fatalf("LoadRubric failed: %v", err)
	}
	return r
}

func makeTranscriptMessage(role, text, turn, provenance, phase string) string {
	itemType := "output_text"
	if role == "user" {
		itemType = "input_text"
	}
	row := map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":    "message",
			"role":    role,
			"phase":   phase,
			"content": []map[string]any{{"type": itemType, "text": text}},
			"internal_chat_message_metadata_passthrough": map[string]any{
				"turn_id": turn,
				"content_item_metadata": []map[string]any{
					{"provenance": map[string]any{"type": provenance}},
				},
			},
		},
	}
	b, _ := json.Marshal(row)
	return string(b)
}

func TestOllamaConnectionFailureAndTimeoutSilentlySkip(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	transcript := filepath.Join(dir, "history.jsonl")
	if err := os.WriteFile(transcript, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	event, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"turn_id":         "current",
		"transcript_path": transcript,
		"prompt":          "Fix it.",
	})
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}),
	}
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	Run(bytes.NewReader(event), &out, rubric, cfg, client)

	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(map[string]any{}, resp); diff != "" {
		t.Fatalf("response mismatch (-want +got):\n%s", diff)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "captures", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 capture file, got %d", len(matches))
	}
	b, _ := os.ReadFile(matches[0])
	var record map[string]any
	_ = json.Unmarshal(b, &record)
	if _, ok := record["skipped"]; !ok {
		t.Fatalf("expected skipped field in capture record, got %v", record)
	}
	if _, ok := record["result"]; ok {
		t.Fatalf("did not expect result in skipped capture record")
	}
}

func TestFormatBlockReasonIsPlainTextWithClarificationsAndMetrics(t *testing.T) {
	rubric := mustLoadRubric(t)
	reason := FormatBlockReason(map[string]float64{
		"intent_clear":       0.85,
		"context_sufficient": 0.25,
		"consistent":         0.92,
	}, rubric, 0.6)
	want := strings.Join([]string{
		"- Specify the target, choose an option, or clarify the expected outcome.",
		"",
		"Intent clarity       0.850",
		"Context sufficiency  0.250",
		"Consistency          0.920",
		"",
		"Threshold            0.600",
	}, "\n")
	if diff := cmp.Diff(want, reason); diff != "" {
		t.Fatalf("reason mismatch (-want +got):\n%s", diff)
	}
}

func TestHarnessAndCurrentTurnExcludedAndToolQuestionsIncluded(t *testing.T) {
	toolCallRow, _ := json.Marshal(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":      "function_call",
			"name":      "request_user_input_async",
			"arguments": `{"questions":[{"title":"Proceed with fix and verification?","options":["Fix and verify","Investigate only"]}]}`,
			"internal_chat_message_metadata_passthrough": map[string]any{
				"turn_id": "turn1",
			},
		},
	})
	snapshot := strings.Join([]string{
		makeTranscriptMessage("user", "env context", "turn0", "harness", ""),
		makeTranscriptMessage("user", "Test failed", "turn1", "user", ""),
		string(toolCallRow),
		makeTranscriptMessage("assistant", "Checking...", "turn1", "user", "commentary"),
		makeTranscriptMessage("user", "Fix and verify", "current", "user", ""),
	}, "\n")
	history, err := HistoryFromSnapshot(snapshot, "current")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []checker.Message{
		{Role: "user", Content: "Test failed"},
		{Role: "assistant", Content: "Proceed with fix and verification? (Fix and verify / Investigate only)"},
	}
	if diff := cmp.Diff(want, history); diff != "" {
		t.Fatalf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestQuestionReplyPromptSkipsModelCall(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	transcript := filepath.Join(dir, "history.jsonl")
	_ = os.WriteFile(transcript, []byte(""), 0o600)
	event, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"turn_id":         "current",
		"transcript_path": transcript,
		"prompt":          "<send_user_message_question_reply>\n[{\"answer\":\"Yes\"}]\n</send_user_message_question_reply>",
	})
	called := false
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("should not call Ollama")
		}),
	}
	var out bytes.Buffer
	Run(bytes.NewReader(event), &out, rubric, checker.Config{Model: "d1:2b", Threshold: 0.6}, client)
	if called {
		t.Fatal("expected structured question reply to skip Ollama call")
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("expected {}, got %s", out.String())
	}
}

func TestOnlyLastSixMessagesAreSent(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, makeTranscriptMessage("user", string(rune('0'+i)), string(rune('a'+i)), "user", ""))
	}
	history, err := HistoryFromSnapshot(strings.Join(lines, "\n"), "current")
	if err != nil {
		t.Fatal(err)
	}
	want := []checker.Message{
		{Role: "user", Content: "4"},
		{Role: "user", Content: "5"},
		{Role: "user", Content: "6"},
		{Role: "user", Content: "7"},
		{Role: "user", Content: "8"},
		{Role: "user", Content: "9"},
	}
	if diff := cmp.Diff(want, history); diff != "" {
		t.Fatalf("history window mismatch (-want +got):\n%s", diff)
	}
}

func TestUnsupportedHistoryIsAnError(t *testing.T) {
	for _, snap := range []string{
		`{"type":"compacted"}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[]}}`,
	} {
		if _, err := HistoryFromSnapshot(snap, "current"); err == nil {
			t.Fatalf("expected error for unsupported snapshot: %s", snap)
		}
	}
}

func TestInvalidEventBlocksInsteadOfSilentlyAllowing(t *testing.T) {
	rubric := mustLoadRubric(t)
	var out bytes.Buffer
	Run(strings.NewReader("{}"), &out, rubric, checker.Config{Model: "d1:2b", Threshold: 0.6}, nil)
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["decision"] != "block" {
		t.Fatalf("expected decision=block, got %v", resp)
	}
}
