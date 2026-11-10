package tui

import (
	"os"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/hook/agy"
	"user-prompt-checker/internal/hook/codex"
)

func LoadContext(rec history.Record) ([]checker.Message, error) {
	path := rec.ResolveTranscriptPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	snapshot := string(data)

	if rec.Agent == "agy" {
		return agy.HistoryFromSnapshot(snapshot, rec.Prompt)
	}
	if rec.Agent == "codex" {
		msgs, _, err := codex.HistoryFromSnapshot(snapshot, rec.SessionID)
		return msgs, err
	}
	return nil, nil
}
