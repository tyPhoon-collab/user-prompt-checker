package hook

import (
	"fmt"
	"strings"

	"user-prompt-checker/internal/checker"
)

func FormatBlockReason(scores map[string]float64, rubric checker.Rubric, threshold float64) string {
	keys := checker.OrderedKeys(rubric)
	var clarifications []string
	var metrics []string
	for _, k := range keys {
		item := rubric[k]
		if scores[k] < threshold {
			clarifications = append(clarifications, "- "+item.Clarification)
		}
		label := item.Label
		if label == "" {
			label = k
		}
		metrics = append(metrics, fmt.Sprintf("%-20s %.3f", label, scores[k]))
	}
	metrics = append(metrics, fmt.Sprintf("\n%-20s %.3f", "Threshold", threshold))
	if len(clarifications) > 0 {
		return strings.Join(clarifications, "\n") + "\n\n" + strings.Join(metrics, "\n")
	}
	return strings.Join(metrics, "\n")
}
