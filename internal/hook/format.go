package hook

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"

	"user-prompt-checker/internal/checker"
)

func FormatBlockReason(scores map[string]float64, rubric checker.Rubric, threshold float64) string {
	keys := checker.OrderedKeys(rubric)
	width := 20
	for _, k := range keys {
		label := rubric[k].Label
		if label == "" {
			label = k
		}
		if w := runewidth.StringWidth(label); w > width {
			width = w
		}
	}

	var clarifications []string
	var metrics []string
	for _, k := range keys {
		item := rubric[k]
		if scores[k] < threshold {
			clarifications = append(clarifications, "  - "+item.Clarification)
		}
		label := item.Label
		if label == "" {
			label = k
		}
		metrics = append(metrics, fmt.Sprintf("%s %.3f", runewidth.FillRight(label, width), scores[k]))
	}
	metrics = append(metrics, fmt.Sprintf("\n%s %.3f", runewidth.FillRight("Threshold", width), threshold))
	if len(clarifications) > 0 {
		return "プロンプトを更新してください\n" + strings.Join(clarifications, "\n") + "\n\n" + strings.Join(metrics, "\n")
	}
	return strings.Join(metrics, "\n")
}
