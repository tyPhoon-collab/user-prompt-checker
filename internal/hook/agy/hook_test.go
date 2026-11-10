package agy

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
	"user-prompt-checker/internal/history"
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

func TestCleanUserPrompt(t *testing.T) {
	raw := "<USER_REQUEST>\nFix the button color.\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\ntime=now\n</ADDITIONAL_METADATA>"
	if got := CleanUserPrompt(raw); got != "Fix the button color." {
		t.Fatalf("expected extracted prompt, got: %q", got)
	}

	plain := "  Hello world  "
	if got := CleanUserPrompt(plain); got != "Hello world" {
		t.Fatalf("expected trimmed prompt, got: %q", got)
	}
}

func TestCountAttachedImages(t *testing.T) {
	withImage := "<USER_REQUEST>\nFix this\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe user has uploaded 2 image(s):\n- /path/1.png\n- /path/2.png\n</ADDITIONAL_METADATA>"
	if got := CountAttachedImages(withImage); got != 2 {
		t.Fatalf("expected 2 images, got: %d", got)
	}

	withoutImage := "<USER_REQUEST>\nFix this\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: now\n</ADDITIONAL_METADATA>"
	if got := CountAttachedImages(withoutImage); got != 0 {
		t.Fatalf("expected 0 images, got: %d", got)
	}
}

func TestHistoryFromSnapshot(t *testing.T) {
	lines := []string{
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","content":"<USER_REQUEST>First prompt</USER_REQUEST>"}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","content":"First answer"}`,
		`{"type":"GENERIC","source":"MODEL","content":"tool log"}`,
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Current prompt"}`,
	}
	history, err := HistoryFromSnapshot(strings.Join(lines, "\n"), "Current prompt")
	if err != nil {
		t.Fatal(err)
	}
	want := []checker.Message{
		{Role: "user", Content: "First prompt"},
		{Role: "assistant", Content: "First answer"},
	}
	if diff := cmp.Diff(want, history); diff != "" {
		t.Fatalf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestRunPassesAttachedImagesToPrompt(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

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

	event, _ := json.Marshal(Event{
		ConversationID: "conv-img",
		LastUserInput:  "<USER_REQUEST>\nFix error\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe user has uploaded 1 image(s):\n- /path/err.png\n</ADDITIONAL_METADATA>",
	})
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	Run(bytes.NewReader(event), &out, rubric, cfg, client)

	wantPrompt := "Fix error\n\n[Attached 1 image(s)]"
	if interceptedPrompt != wantPrompt {
		t.Fatalf("expected evaluated prompt %q, got %q", wantPrompt, interceptedPrompt)
	}
	records, err := history.LoadAll("")
	if err != nil || len(records) != 1 {
		t.Fatalf("unexpected saved history: %+v, %v", records, err)
	}
	if records[0].Prompt != "Fix error" || records[0].TranscriptBytes == nil || *records[0].TranscriptBytes != 0 {
		t.Fatalf("expected original prompt and empty transcript boundary: %+v", records[0])
	}
}

func TestOllamaConnectionFailureSilentlySkips(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	_ = os.WriteFile(transcript, []byte(""), 0o600)
	event, _ := json.Marshal(Event{
		ConversationID:        "conv1",
		TranscriptPath:        transcript,
		ArtifactDirectoryPath: filepath.Join(dir, "artifacts"),
		LastUserInput:         "Fix it.",
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
	if len(resp) != 0 {
		t.Fatalf("expected empty response on connection error, got %v", resp)
	}
}

func TestBlockReturnsTerminateAndErrorMessage(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	event, _ := json.Marshal(Event{
		ConversationID: "conv1",
		LastUserInput:  "Fix it.",
	})
	mockBody := `{"answers":{"intent_clear":{"type":"noul","noul":0.9},"context_sufficient":{"type":"noul","noul":0.1},"consistent":{"type":"noul","noul":0.9}}}`
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(mockBody)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	_ = os.Setenv("HOME", dir)
	Run(bytes.NewReader(event), &out, rubric, cfg, client)

	var resp struct {
		TerminationBehavior string `json:"terminationBehavior"`
		InjectSteps         []struct {
			ErrorMessage struct {
				UserMessage string `json:"userMessage"`
			} `json:"errorMessage"`
		} `json:"injectSteps"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TerminationBehavior != "terminate" {
		t.Fatalf("expected terminate, got %q", resp.TerminationBehavior)
	}
	if len(resp.InjectSteps) != 1 || !strings.Contains(resp.InjectSteps[0].ErrorMessage.UserMessage, "Context sufficiency") {
		t.Fatalf("unexpected injectSteps: %+v", resp.InjectSteps)
	}
}

func TestHistoryFromSnapshotMalformedLineFails(t *testing.T) {
	lines := []string{
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","content":"First prompt"}`,
		`{invalid json line`,
	}
	_, err := HistoryFromSnapshot(strings.Join(lines, "\n"), "Second prompt")
	if err == nil {
		t.Fatal("expected error on malformed JSON line, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse transcript line") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestMalformedTranscriptBlocksInvocation(t *testing.T) {
	rubric := mustLoadRubric(t)
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("not a valid json line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	event, _ := json.Marshal(Event{
		ConversationID: "conv-err",
		TranscriptPath: transcript,
		LastUserInput:  "Fix it.",
	})
	var out bytes.Buffer
	cfg := checker.Config{Model: "d1:2b", URL: "http://localhost:11434", Threshold: 0.6, Timeout: time.Second}
	Run(bytes.NewReader(event), &out, rubric, cfg, nil)

	var resp struct {
		TerminationBehavior string `json:"terminationBehavior"`
		InjectSteps         []struct {
			ErrorMessage struct {
				UserMessage string `json:"userMessage"`
			} `json:"errorMessage"`
		} `json:"injectSteps"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TerminationBehavior != "terminate" {
		t.Fatalf("expected terminate on malformed transcript, got %q", resp.TerminationBehavior)
	}
	if len(resp.InjectSteps) == 0 || !strings.Contains(resp.InjectSteps[0].ErrorMessage.UserMessage, "Prompt check failed") {
		t.Fatalf("expected failure message, got %+v", resp.InjectSteps)
	}
}

func TestGoldenTranscriptSnapshotExtraction(t *testing.T) {
	goldenTranscript := strings.Join([]string{
		`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-07T18:24:24Z","content":"<USER_REQUEST>\nログインボタンの色を青にしたい\n</USER_REQUEST>"}`,
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-07T18:24:25Z","content":"承知しました。#0066ccに変更する案はいかがでしょうか？","tool_calls":[{"name":"read_file","args":{"Path":"style.css"}}]}`,
		`{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-11-07T18:24:26Z","content":"file content"}`,
		`{"step_index":3,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-07T18:24:30Z","content":"<USER_REQUEST>\nそれで進めて。\n</USER_REQUEST>"}`,
	}, "\n")

	history, err := HistoryFromSnapshot(goldenTranscript, "それで進めて。")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []checker.Message{
		{Role: "user", Content: "ログインボタンの色を青にしたい"},
		{Role: "assistant", Content: "承知しました。#0066ccに変更する案はいかがでしょうか？"},
	}
	if diff := cmp.Diff(want, history); diff != "" {
		t.Fatalf("extracted history mismatch (-want +got):\n%s", diff)
	}
}
