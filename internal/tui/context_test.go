package tui_test

import (
	"os"
	"path/filepath"
	"testing"

	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/tui"
)

func TestContextUsesEvaluationBoundary(t *testing.T) {
	for _, agent := range []string{"codex", "agy"} {
		t.Run(agent, func(t *testing.T) {
			prior := `{"type":"USER_INPUT","content":"prior"}` + "\n"
			current := `{"type":"USER_INPUT","content":"current"}` + "\n"
			future := `{"type":"USER_INPUT","content":"future"}` + "\n"
			if agent == "codex" {
				prior = `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"prior"}],"internal_chat_message_metadata_passthrough":{"turn_id":"before"}}}` + "\n"
				current = `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"current"}],"internal_chat_message_metadata_passthrough":{"turn_id":"target"}}}` + "\n"
				future = `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"future"}],"internal_chat_message_metadata_passthrough":{"turn_id":"after"}}}` + "\n"
			}
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(path, []byte(prior+current+future), 0600); err != nil {
				t.Fatal(err)
			}
			size := int64(len(prior + current))
			rec := history.Record{Agent: agent, SessionID: "conversation", TurnID: "target", Prompt: "current", TranscriptPath: path, TranscriptBytes: &size}
			messages, err := tui.LoadContext(rec)
			if err != nil || len(messages) != 1 || messages[0].Content != "prior" {
				t.Fatalf("expected only prior context, got %+v, %v", messages, err)
			}
			rec.TranscriptBytes = nil
			if messages, err := tui.LoadContext(rec); err != nil || len(messages) != 0 {
				t.Fatalf("legacy context must not guess a boundary: %+v, %v", messages, err)
			}
			rec.TranscriptBytes = &size
			if err := os.WriteFile(path, []byte(prior), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := tui.LoadContext(rec); err == nil {
				t.Fatal("expected error for truncated transcript")
			}
		})
	}
}
