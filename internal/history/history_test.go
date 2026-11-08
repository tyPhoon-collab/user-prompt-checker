package history_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"user-prompt-checker/internal/history"
)

func TestAppendAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	rec1 := history.Record{
		Timestamp: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
		SessionID: "sess-1",
		Agent:     "agy",
		Prompt:    "First test prompt",
		Pass:      true,
		Scores: map[string]float64{
			"intent_clear": 0.85,
		},
		ElapsedMS: 120.5,
	}

	rec2 := history.Record{
		Timestamp: time.Date(2026, 11, 8, 10, 5, 0, 0, time.UTC),
		SessionID: "sess-1",
		Agent:     "agy",
		Prompt:    "Second test prompt",
		Pass:      false,
		Scores: map[string]float64{
			"intent_clear": 0.35,
		},
		Failed:         []string{"intent_clear"},
		Clarifications: []string{"Specify the target"},
		ElapsedMS:      98.2,
	}

	// 1. レコード追加
	if err := history.Append(rec1, tmpDir); err != nil {
		t.Fatalf("Append rec1 failed: %v", err)
	}
	if err := history.Append(rec2, tmpDir); err != nil {
		t.Fatalf("Append rec2 failed: %v", err)
	}

	// 2. ファイル名が history-2026-11.jsonl であることを確認
	expectedFile := filepath.Join(tmpDir, "history-2026-11.jsonl")
	info, err := os.Stat(expectedFile)
	if err != nil {
		t.Fatalf("expected monthly jsonl file %s to exist: %v", expectedFile, err)
	}

	// サイズがコンパクト（1レコード数百バイト）であることを確認
	if info.Size() > 2000 {
		t.Errorf("file size is unexpectedly large: %d bytes", info.Size())
	}

	// 3. 読み込み検証
	records, err := history.LoadAll(tmpDir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Prompt != "First test prompt" || !records[0].Pass {
		t.Errorf("record 0 mismatch: %+v", records[0])
	}
	if records[1].Prompt != "Second test prompt" || records[1].Pass {
		t.Errorf("record 1 mismatch: %+v", records[1])
	}
}
