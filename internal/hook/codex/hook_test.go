package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

func makeTranscriptMessageWithImages(role, text, turn string, imageCount int) string {
	content := []map[string]any{}
	if text != "" {
		content = append(content, map[string]any{"type": "input_text", "text": text})
	}
	for i := 0; i < imageCount; i++ {
		content = append(content, map[string]any{
			"type": "input_image",
			"image_url": map[string]any{"url": "data:image/png;base64,..."},
		})
	}
	row := map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":    "message",
			"role":    role,
			"content": content,
			"internal_chat_message_metadata_passthrough": map[string]any{
				"turn_id": turn,
			},
		},
	}
	b, _ := json.Marshal(row)
	return string(b)
}

func TestCountAttachedImages(t *testing.T) {
	lines := []string{
		makeTranscriptMessageWithImages("user", "prev message", "turn0", 1),
		makeTranscriptMessageWithImages("user", "current message", "current", 2),
	}
	snapshot := strings.Join(lines, "\n")
	if got := CountAttachedImages(snapshot, "current"); got != 2 {
		t.Fatalf("expected 2 images for current turn, got: %d", got)
	}
	if got := CountAttachedImages(snapshot, "nonexistent"); got != 0 {
		t.Fatalf("expected 0 images for nonexistent turn, got: %d", got)
	}
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
	history, _, err := HistoryFromSnapshot(snapshot, "current")
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
	history, _, err := HistoryFromSnapshot(strings.Join(lines, "\n"), "current")
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
	snap := `{"type":"compacted"}`
	if _, _, err := HistoryFromSnapshot(snap, "current"); err == nil {
		t.Fatalf("expected error for unsupported snapshot: %s", snap)
	}
}

func TestUserMessageWithoutContentItemMetadataIsExtracted(t *testing.T) {
	// content_item_metadata が存在しない一般的なユーザーメッセージ行
	line := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Hello without metadata"}]}}`
	history, warnings, err := HistoryFromSnapshot(line, "other_turn")
	if err != nil {
		t.Fatalf("unexpected error when metadata is missing: %v", err)
	}
	if len(history) != 1 || history[0].Content != "Hello without metadata" {
		t.Fatalf("unexpected history extracted: %+v", history)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "user provenance metadata missing") {
		t.Fatalf("expected warning for missing provenance metadata, got %v", warnings)
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

func TestRunPassesAttachedImagesToPrompt(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	transcript := filepath.Join(dir, "history.jsonl")
	line := makeTranscriptMessageWithImages("user", "Fix it", "current", 1)
	if err := os.WriteFile(transcript, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	var interceptedPrompt string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body struct {
				State struct {
					UserPrompt string `json:"user_prompt"`
				} `json:"state"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			interceptedPrompt = body.State.UserPrompt

			resp := map[string]any{
				"answers": map[string]any{
					"intent_clear":      map[string]any{"type": "noul", "noul": 0.95},
					"context_sufficient": map[string]any{"type": "noul", "noul": 0.95},
					"consistent":        map[string]any{"type": "noul", "noul": 0.95},
				},
			}
			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(b)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	event, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"turn_id":         "current",
		"transcript_path": transcript,
		"prompt":          "Fix it",
	})
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	Run(bytes.NewReader(event), &out, rubric, cfg, client)

	wantPrompt := "Fix it\n\n[Attached 1 image(s)]"
	if interceptedPrompt != wantPrompt {
		t.Fatalf("expected evaluated prompt %q, got %q", wantPrompt, interceptedPrompt)
	}
}

func TestMissingProvenanceInjectsWarningContextInRun(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	transcript := filepath.Join(dir, "history.jsonl")
	// content_item_metadata なしの行
	line := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"prev prompt"}]}}`
	if err := os.WriteFile(transcript, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp := map[string]any{
				"answers": map[string]any{
					"intent_clear":       map[string]any{"type": "noul", "noul": 0.95},
					"context_sufficient": map[string]any{"type": "noul", "noul": 0.95},
					"consistent":         map[string]any{"type": "noul", "noul": 0.95},
				},
			}
			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(b)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	event, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"turn_id":         "current",
		"transcript_path": transcript,
		"prompt":          "Fix it",
	})
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	Run(bytes.NewReader(event), &out, rubric, cfg, client)

	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	hso, ok := resp["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("expected hookSpecificOutput in response, got %v", resp)
	}
	ctx, _ := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "user provenance metadata missing") {
		t.Fatalf("expected warning in additionalContext, got %q", ctx)
	}
}

func TestEmptyTranscriptPathAllowsExecution(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp := map[string]any{
				"answers": map[string]any{
					"intent_clear":       map[string]any{"type": "noul", "noul": 0.95},
					"context_sufficient": map[string]any{"type": "noul", "noul": 0.95},
					"consistent":         map[string]any{"type": "noul", "noul": 0.95},
				},
			}
			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(b)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	// transcript_path が空文字（/side など）
	event, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"turn_id":         "current",
		"transcript_path": "",
		"prompt":          "What does this function do?",
	})
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	Run(bytes.NewReader(event), &out, rubric, cfg, client)

	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// エラーで block されず、正常に allow（空レスポンス）であること
	if resp["decision"] == "block" {
		t.Fatalf("expected non-block response when transcript_path is empty, got %v", resp)
	}
}
