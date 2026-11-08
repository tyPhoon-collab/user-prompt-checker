package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"user-prompt-checker/internal/checker"
)

type Case struct {
	ID       string            `json:"id" yaml:"id"`
	Source   string            `json:"source,omitempty" yaml:"source,omitempty"`
	Prompt   string            `json:"prompt" yaml:"prompt"`
	History  []checker.Message `json:"history,omitempty" yaml:"history,omitempty"`
	Expected map[string]bool   `json:"expected" yaml:"expected"`
}

type Row struct {
	checker.Result
	ID       string          `json:"id"`
	Source   string          `json:"source"`
	Expected map[string]bool `json:"expected"`
}

type Summary struct {
	Count         int                `json:"count"`
	Threshold     float64            `json:"threshold"`
	GateMatches   int                `json:"gate_matches"`
	FalseBlocks   []string           `json:"false_blocks"`
	FalsePasses   []string           `json:"false_passes"`
	AspectMatches map[string]int     `json:"aspect_matches"`
	LatencyMS     map[string]float64 `json:"latency_ms"`
}

func Summarize(rows []Row, rubric checker.Rubric, threshold float64) Summary {
	falseBlocks := []string{}
	falsePasses := []string{}
	gateMatches := 0
	aspectMatches := make(map[string]int, len(rubric))
	for k := range rubric {
		aspectMatches[k] = 0
	}
	times := make([]float64, len(rows))
	for i, row := range rows {
		times[i] = row.ElapsedMS
		expPass := true
		for k := range rubric {
			if !row.Expected[k] {
				expPass = false
			}
			if (row.Scores[k] >= threshold) == row.Expected[k] {
				aspectMatches[k]++
			}
		}
		if row.Pass == expPass {
			gateMatches++
		} else if expPass && !row.Pass {
			falseBlocks = append(falseBlocks, row.ID)
		} else if !expPass && row.Pass {
			falsePasses = append(falsePasses, row.ID)
		}
	}
	first, median, maxVal := 0.0, 0.0, 0.0
	if len(times) > 0 {
		first = times[0]
		sorted := append([]float64(nil), times...)
		sort.Float64s(sorted)
		maxVal = sorted[len(sorted)-1]
		mid := len(sorted) / 2
		if len(sorted)%2 == 1 {
			median = sorted[mid]
		} else {
			median = (sorted[mid-1] + sorted[mid]) / 2.0
		}
	}
	return Summary{
		Count:         len(rows),
		Threshold:     threshold,
		GateMatches:   gateMatches,
		FalseBlocks:   falseBlocks,
		FalsePasses:   falsePasses,
		AspectMatches: aspectMatches,
		LatencyMS:     map[string]float64{"first": first, "median": median, "max": maxVal},
	}
}

func Run(casesPath, outputPath string, rubric checker.Rubric, cfg checker.Config, stdout io.Writer, client *http.Client) error {
	b, err := os.ReadFile(casesPath)
	if err != nil {
		return err
	}
	var cases []Case
	if err := yaml.Unmarshal(b, &cases); err != nil {
		return fmt.Errorf("failed to parse cases file: %w", err)
	}
	if len(cases) == 0 {
		return errors.New("cases must be a non-empty YAML/JSON array")
	}
	keys := checker.OrderedKeys(rubric)
	seenIDs := make(map[string]bool, len(cases))
	for _, c := range cases {
		if c.ID == "" || seenIDs[c.ID] {
			return fmt.Errorf("duplicate or empty case ID: %s", c.ID)
		}
		seenIDs[c.ID] = true
		if len(c.Expected) != len(rubric) {
			return fmt.Errorf("expected criteria mismatch in case %s", c.ID)
		}
		for _, k := range keys {
			if _, ok := c.Expected[k]; !ok {
				return fmt.Errorf("missing expected key %s in case %s", k, c.ID)
			}
		}
		if _, err := checker.MakePayload(c.Prompt, c.History, rubric, cfg.Model); err != nil {
			return fmt.Errorf("case %s: %w", c.ID, err)
		}
	}

	header := fmt.Sprintf("%-36s", "case")
	for _, k := range keys {
		header += fmt.Sprintf(" %19s", k)
	}
	header += " gate      ms"
	fmt.Fprintln(stdout, header)

	rows := make([]Row, 0, len(cases))
	for _, c := range cases {
		res, err := checker.Check(c.Prompt, c.History, rubric, cfg, client)
		if err != nil {
			return fmt.Errorf("case %s: %w", c.ID, err)
		}
		src := c.Source
		if src == "" {
			src = "synthetic"
		}
		row := Row{
			Result:   res,
			ID:       c.ID,
			Source:   src,
			Expected: c.Expected,
		}
		rows = append(rows, row)
		line := fmt.Sprintf("%-36s", row.ID)
		for _, k := range keys {
			line += fmt.Sprintf(" %19.3f", row.Scores[k])
		}
		gate := "BLOCK"
		if row.Pass {
			gate = "PASS"
		}
		line += fmt.Sprintf(" %-5s %6.0f", gate, row.ElapsedMS)
		fmt.Fprintln(stdout, line)
	}

	summary := Summarize(rows, rubric, cfg.Threshold)
	report := map[string]any{
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"model":      cfg.Model,
		"url":        cfg.URL,
		"rubric":     rubric,
		"note":       "Expected values are provisional hypotheses prior to independent human review. Execution order is fixed.",
		"summary":    summary,
		"results":    rows,
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, append(reportBytes, '\n'), 0o644); err != nil {
		return err
	}
	sumBytes, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Fprintln(stdout, string(sumBytes))
	fmt.Fprintf(stdout, "Saved: %s\n", outputPath)
	return nil
}
