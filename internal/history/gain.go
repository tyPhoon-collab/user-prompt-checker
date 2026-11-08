package history

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

type GainPair struct {
	Before Record
	After  Record
	Gain   float64
}

type Summary struct {
	TotalPrompts int
	PassedCount  int
	PassRate     float64
	AvgScores    map[string]float64
	GainPairs    []GainPair
	TopPrompts   []Record
	AllPrompts   []Record
}

func AvgScore(scores map[string]float64) float64 {
	if len(scores) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range scores {
		sum += s
	}
	return sum / float64(len(scores))
}

func Analyze(records []Record) Summary {
	sum := Summary{
		TotalPrompts: len(records),
		AvgScores:    make(map[string]float64),
		AllPrompts:   records,
	}
	if len(records) == 0 {
		return sum
	}

	scoreSums := make(map[string]float64)
	scoreCounts := make(map[string]int)

	bySession := make(map[string][]Record)
	var passed []Record

	for _, r := range records {
		if r.Pass {
			sum.PassedCount++
			passed = append(passed, r)
		}
		for k, v := range r.Scores {
			scoreSums[k] += v
			scoreCounts[k]++
		}
		if r.SessionID != "" {
			bySession[r.SessionID] = append(bySession[r.SessionID], r)
		}
	}

	sum.PassRate = float64(sum.PassedCount) / float64(sum.TotalPrompts) * 100.0

	for k, total := range scoreSums {
		if cnt := scoreCounts[k]; cnt > 0 {
			sum.AvgScores[k] = total / float64(cnt)
		}
	}

	// セッション内での Gain ペア検出 (直前の Block -> 次の Pass)
	for _, sessRecords := range bySession {
		for i := 0; i < len(sessRecords)-1; i++ {
			prev := sessRecords[i]
			curr := sessRecords[i+1]
			if !prev.Pass && curr.Pass {
				prevAvg := AvgScore(prev.Scores)
				currAvg := AvgScore(curr.Scores)
				gain := currAvg - prevAvg
				sum.GainPairs = append(sum.GainPairs, GainPair{
					Before: prev,
					After:  curr,
					Gain:   math.Round(gain*1000) / 1000,
				})
			}
		}
	}

	// Top Prompts: 通過プロンプトのうち平均スコア上位5件
	sort.Slice(passed, func(i, j int) bool {
		return AvgScore(passed[i].Scores) > AvgScore(passed[j].Scores)
	})
	limit := 5
	if len(passed) < limit {
		limit = len(passed)
	}
	sum.TopPrompts = passed[:limit]

	return sum
}

func RenderGainReport(sum Summary, w io.Writer) {
	fmt.Fprintln(w, "PROMPT QUALITY GAIN REPORT")
	fmt.Fprintln(w, strings.Repeat("─", 70))
	fmt.Fprintf(w, "Total Prompts : %d\n", sum.TotalPrompts)
	fmt.Fprintf(w, "Pass Rate     : %.1f%% (%d passed, %d blocked)\n", sum.PassRate, sum.PassedCount, sum.TotalPrompts-sum.PassedCount)

	if len(sum.AvgScores) > 0 {
		fmt.Fprintln(w, "\nAverage Scores by Criterion:")
		keys := make([]string, 0, len(sum.AvgScores))
		for k := range sum.AvgScores {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "  %-24s : %.3f\n", k, sum.AvgScores[k])
		}
	}

	if len(sum.GainPairs) > 0 {
		fmt.Fprintf(w, "\nDetected Prompt Gains (%d improvements):\n", len(sum.GainPairs))
		for i, gp := range sum.GainPairs {
			sign := "+"
			if gp.Gain < 0 {
				sign = ""
			}
			fmt.Fprintf(w, "  %d. [%s%.3f Gain] Session: %s\n", i+1, sign, gp.Gain, gp.Before.SessionID)
			fmt.Fprintf(w, "     [-] Blocked (avg %.3f): %q\n", AvgScore(gp.Before.Scores), truncate(gp.Before.Prompt, 60))
			fmt.Fprintf(w, "     [+] Passed  (avg %.3f): %q\n", AvgScore(gp.After.Scores), truncate(gp.After.Prompt, 60))
		}
	}

	if len(sum.TopPrompts) > 0 {
		fmt.Fprintln(w, "\nTop Performed Prompts:")
		for _, tp := range sum.TopPrompts {
			fmt.Fprintf(w, "  ★ %.3f | %q\n", AvgScore(tp.Scores), truncate(tp.Prompt, 65))
		}
	}
	fmt.Fprintln(w, strings.Repeat("─", 70))
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen-3]) + "..."
	}
	return s
}
