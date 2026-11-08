package eval

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"user-prompt-checker/internal/checker"
)

func TestSummaryDistinguishesFalseBlocksAndFalsePasses(t *testing.T) {
	rubric := checker.Rubric{"intent_clear": {}}
	rows := []Row{
		{
			ID:       "good",
			Expected: map[string]bool{"intent_clear": true},
			Result: checker.Result{
				Pass:      false,
				Scores:    map[string]float64{"intent_clear": 0.2},
				ElapsedMS: 10,
			},
		},
		{
			ID:       "bad",
			Expected: map[string]bool{"intent_clear": false},
			Result: checker.Result{
				Pass:      true,
				Scores:    map[string]float64{"intent_clear": 0.9},
				ElapsedMS: 20,
			},
		},
	}
	want := Summary{
		Count:         2,
		Threshold:     0.6,
		GateMatches:   0,
		FalseBlocks:   []string{"good"},
		FalsePasses:   []string{"bad"},
		AspectMatches: map[string]int{"intent_clear": 0},
		LatencyMS:     map[string]float64{"first": 10, "median": 15, "max": 20},
	}
	if diff := cmp.Diff(want, Summarize(rows, rubric, 0.6)); diff != "" {
		t.Fatalf("summary mismatch (-want +got):\n%s", diff)
	}
}
