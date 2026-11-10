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
