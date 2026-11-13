package hook

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"user-prompt-checker/internal/checker"
)

func TestFormatBlockReasonIsPlainTextWithClarificationsAndMetrics(t *testing.T) {
	rubric, err := checker.LoadRubric("")
	if err != nil {
		t.Fatal(err)
	}
	reason := FormatBlockReason(map[string]float64{
		"intent_clear":       0.85,
		"context_sufficient": 0.25,
		"consistent":         0.92,
	}, rubric, 0.6)
	want := strings.Join([]string{
		"プロンプトを更新してください",
		"  - 対象の指定、選択肢の選択、または期待する結果を具体化してください。",
		"",
		"意図の明確さ         0.850",
		"文脈の充足度         0.250",
		"整合性               0.920",
		"",
		"Threshold            0.600",
	}, "\n")
	if diff := cmp.Diff(want, reason); diff != "" {
		t.Fatalf("reason mismatch (-want +got):\n%s", diff)
	}
}
