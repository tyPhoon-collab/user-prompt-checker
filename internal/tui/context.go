package tui

import (
	"fmt"
	"io"
	"os"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/hook/agy"
	"user-prompt-checker/internal/hook/codex"
)

func LoadContext(rec history.Record) ([]checker.Message, error) {
	// Older records have no reliable boundary for the evaluation context.
	if rec.TranscriptBytes == nil {
		return nil, nil
	}
	if *rec.TranscriptBytes < 0 {
		return nil, fmt.Errorf("invalid transcript boundary")
	}
	path := rec.ResolveTranscriptPath()
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, *rec.TranscriptBytes))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != *rec.TranscriptBytes {
		return nil, fmt.Errorf("transcript truncated since evaluation")
	}
	snapshot := string(data)

	if rec.Agent == "agy" {
		return agy.HistoryFromSnapshot(snapshot, rec.Prompt)
	}
	if rec.Agent == "codex" {
		msgs, _, err := codex.HistoryFromSnapshot(snapshot, rec.TurnID)
		return msgs, err
	}
	return nil, nil
}
