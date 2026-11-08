package history_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"user-prompt-checker/internal/history"
)

func TestAnalyzeAndRenderGain(t *testing.T) {
	records := []history.Record{
		{
			Timestamp: time.Now().Add(-10 * time.Minute),
			SessionID: "sess-1",
			Prompt:    "fix it",
			Pass:      false,
			Scores:    map[string]float64{"intent_clear": 0.3},
		},
		{
			Timestamp: time.Now().Add(-5 * time.Minute),
			SessionID: "sess-1",
			Prompt:    "fix the login button background color to #0066cc",
			Pass:      true,
			Scores:    map[string]float64{"intent_clear": 0.9},
		},
		{
			Timestamp: time.Now().Add(-1 * time.Minute),
			SessionID: "sess-2",
			Prompt:    "run tests",
			Pass:      true,
			Scores:    map[string]float64{"intent_clear": 0.7},
		},
	}

	sum := history.Analyze(records)
	if sum.TotalPrompts != 3 {
		t.Fatalf("expected 3 total prompts, got %d", sum.TotalPrompts)
	}
	if sum.PassedCount != 2 {
		t.Fatalf("expected 2 passed, got %d", sum.PassedCount)
	}
	if len(sum.GainPairs) != 1 {
		t.Fatalf("expected 1 gain pair, got %d", len(sum.GainPairs))
	}
	if sum.GainPairs[0].Gain != 0.6 {
		t.Errorf("expected gain of 0.6, got %f", sum.GainPairs[0].Gain)
	}

	var buf bytes.Buffer
	history.RenderGainReport(sum, &buf)
	out := buf.String()
	if !strings.Contains(out, "PROMPT QUALITY GAIN REPORT") {
		t.Errorf("report missing header:\n%s", out)
	}
	if !strings.Contains(out, "+0.600 Gain") {
		t.Errorf("report missing +0.600 Gain:\n%s", out)
	}
}
