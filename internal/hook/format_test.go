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
		"complete":   0.85,
		"actionable": 0.25,
	}, rubric, 0.6)
	want := strings.Join([]string{
		"プロンプトを更新してください",
		"  - 対象の指定、選択肢の選択、または矛盾する条件の解消を行ってください。",
		"",
		"入力の完結性         0.850",
		"対応の妥当性         0.250",
		"",
		"Threshold            0.600",
	}, "\n")
	if diff := cmp.Diff(want, reason); diff != "" {
		t.Fatalf("reason mismatch (-want +got):\n%s", diff)
	}
}
