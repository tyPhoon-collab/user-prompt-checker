package hook

import (
	"slices"
	"strings"

	"user-prompt-checker/internal/checker"
)

const (
	// MaxHistoryMessages limits conversation context to the last 10 messages
	// while MaxMessageRunes bounds per-message length so the local 2B model keeps low latency.
	MaxHistoryMessages = 10
	MaxMessageRunes    = 1000
	MaxEditedFiles     = 5
)

func TrimHistory(history []checker.Message) []checker.Message {
	if len(history) <= MaxHistoryMessages {
		return history
	}
	return history[len(history)-MaxHistoryMessages:]
}

func TruncateContent(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	headLen := maxRunes / 3
	tailLen := maxRunes - headLen
	return strings.TrimSpace(string(runes[:headLen])) + "\n...\n" + strings.TrimSpace(string(runes[len(runes)-tailLen:]))
}

func AppendEditedFiles(content string, files []string) string {
	unique := make([]string, 0, len(files))
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" || slices.Contains(unique, f) {
			continue
		}
		unique = append(unique, f)
	}
	if len(unique) == 0 {
		return content
	}
	extra := ""
	if len(unique) > MaxEditedFiles {
		unique = unique[:MaxEditedFiles]
		extra = ", ..."
	}
	line := "[Edited files: " + strings.Join(unique, ", ") + extra + "]"
	if strings.TrimSpace(content) == "" {
		return line
	}
	return content + "\n" + line
}

func FlushTurnEdits(history []checker.Message, hasAssistantInTurn bool, edited []string) []checker.Message {
	if len(edited) == 0 {
		return history
	}
	if hasAssistantInTurn && len(history) > 0 && history[len(history)-1].Role == "assistant" {
		history[len(history)-1].Content = AppendEditedFiles(history[len(history)-1].Content, edited)
		return history
	}
	return append(history, checker.Message{
		Role:    "assistant",
		Content: AppendEditedFiles("", edited),
	})
}
