package agy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"user-prompt-checker/internal/checker"
)

var userRequestRegex = regexp.MustCompile(`(?s)<USER_REQUEST>\s*(.*?)\s*</USER_REQUEST>`)

type Event struct {
	ConversationID        string   `json:"conversationId"`
	WorkspacePaths        []string `json:"workspacePaths"`
	TranscriptPath        string   `json:"transcriptPath"`
	ArtifactDirectoryPath string   `json:"artifactDirectoryPath"`
	ModelName             string   `json:"modelName"`
	LastUserInput         string   `json:"lastUserInput"`
	InvocationNum         int      `json:"invocationNum"`
	InitialNumSteps       int      `json:"initialNumSteps"`
}

func CleanUserPrompt(raw string) string {
	matches := userRequestRegex.FindStringSubmatch(raw)
	if len(matches) > 1 && strings.TrimSpace(matches[1]) != "" {
		return strings.TrimSpace(matches[1])
	}
	return strings.TrimSpace(raw)
}

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

func HistoryFromSnapshot(snapshot string, currentPrompt string) ([]checker.Message, error) {
	history := []checker.Message{}
	cleanedCurrent := CleanUserPrompt(currentPrompt)

	for _, line := range strings.Split(snapshot, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			Source  string `json:"source"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(trimmed), &row); err != nil {
			return nil, fmt.Errorf("failed to parse transcript line: %w", err)
		}
		if row.Type == "USER_INPUT" {
			prompt := CleanUserPrompt(row.Content)
			if prompt == "" || prompt == cleanedCurrent {
				continue
			}
			history = append(history, checker.Message{Role: "user", Content: prompt})
		} else if row.Type == "PLANNER_RESPONSE" {
			content := strings.TrimSpace(row.Content)
			if content != "" {
				history = append(history, checker.Message{Role: "assistant", Content: content})
			}
		}
	}
	if len(history) > 6 {
		history = history[len(history)-6:]
	}
	return history, nil
}

func Run(stdin io.Reader, stdout io.Writer, rubric checker.Rubric, cfg checker.Config, client *http.Client) {
	record := map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	var outputPath string
	response := map[string]any{}

	err := func() error {
		var event Event
		if err := json.NewDecoder(stdin).Decode(&event); err != nil {
			return err
		}
		record["event"] = event
		if event.LastUserInput == "" {
			return errors.New("missing lastUserInput in hook event")
		}

		capturesDir := ""
		if event.ArtifactDirectoryPath != "" {
			capturesDir = filepath.Join(filepath.Dir(event.ArtifactDirectoryPath), "captures")
		} else {
			home, err := os.UserHomeDir()
			if err == nil {
				capturesDir = filepath.Join(home, ".gemini", "antigravity-cli", "captures")
			}
		}
		if capturesDir != "" {
			_ = os.MkdirAll(capturesDir, 0o700)
			outputPath = filepath.Join(capturesDir, uuid.NewString()+".json")
		}

		prompt := CleanUserPrompt(event.LastUserInput)
		if prompt == "" {
			return errors.New("empty user prompt after extraction")
		}

		var history []checker.Message
		if event.TranscriptPath != "" {
			snapshotBytes, err := os.ReadFile(event.TranscriptPath)
			if err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("failed to read transcript: %w", err)
				}
			} else {
				snapshot := string(snapshotBytes)
				record["transcript_snapshot"] = snapshot
				h, err := HistoryFromSnapshot(snapshot, prompt)
				if err != nil {
					return err
				}
				history = h
			}
		}

		result, err := checker.Check(prompt, history, rubric, cfg, client)
		if err != nil {
			if checker.IsNetworkError(err) {
				record["skipped"] = "Ollama unavailable: " + err.Error()
				return nil
			}
			return err
		}
		record["result"] = result
		if !result.Pass {
			blockReason := FormatBlockReason(result.Scores, rubric, cfg.Threshold)
			response = map[string]any{
				"terminationBehavior": "terminate",
				"injectSteps": []map[string]any{
					{
						"errorMessage": map[string]any{
							"userMessage": blockReason,
						},
					},
				},
			}
		}
		return nil
	}()

	if err != nil {
		record["error"] = err.Error()
		response = map[string]any{
			"terminationBehavior": "terminate",
			"injectSteps": []map[string]any{
				{
					"errorMessage": map[string]any{
						"userMessage": "Prompt check failed: " + err.Error(),
					},
				},
			},
		}
	}

	record["hook_response"] = response
	if outputPath != "" {
		data, err := json.MarshalIndent(record, "", "  ")
		if err == nil {
			_ = os.WriteFile(outputPath, append(data, '\n'), 0o600)
		}
	}

	out, _ := json.Marshal(response)
	fmt.Fprintln(stdout, string(out))
}
