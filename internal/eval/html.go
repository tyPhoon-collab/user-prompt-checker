package eval

import (
	"bytes"
	_ "embed"
	"html/template"

	"user-prompt-checker/internal/checker"
)

//go:embed report.html
var reportHTML string

func expectedPass(expected map[string]bool) bool {
	for _, pass := range expected {
		if !pass {
			return false
		}
	}
	return true
}

func renderHTML(report report) ([]byte, error) {
	keys := checker.OrderedKeys(report.Rubric)
	mismatch := func(row Row) bool {
		if row.Pass != expectedPass(row.Expected) {
			return true
		}
		for _, key := range keys {
			if (row.Scores[key] >= report.Summary.Threshold) != row.Expected[key] {
				return true
			}
		}
		return false
	}
	count := 0
	for _, row := range report.Results {
		if mismatch(row) {
			count++
		}
	}
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"keys":          func() []string { return keys },
		"mismatch":      mismatch,
		"mismatchCount": func() int { return count },
		"expectedPass":  expectedPass,
	}).Parse(reportHTML)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, report); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
