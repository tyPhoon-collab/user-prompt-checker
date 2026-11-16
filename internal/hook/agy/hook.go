package agy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/hook"
)

var userRequestRegex = regexp.MustCompile(`(?s)<USER_REQUEST>\s*(.*?)\s*</USER_REQUEST>`)
var uploadedImagesRegex = regexp.MustCompile(`The user has uploaded (\d+)\s+image\(s\):`)
var skillTagRegex = regexp.MustCompile(`(?s)<skill>.*?</skill>`)

func stripSkillTags(s string) string {
	return strings.TrimSpace(skillTagRegex.ReplaceAllString(s, ""))
}

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
		return stripSkillTags(strings.TrimSpace(matches[1]))
	}
	return stripSkillTags(strings.TrimSpace(raw))
}

func CountAttachedImages(raw string) int {
	matches := uploadedImagesRegex.FindStringSubmatch(raw)
	if len(matches) > 1 {
		if n, err := strconv.Atoi(matches[1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func unquoteArg(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		var unq string
		if err := json.Unmarshal([]byte(s), &unq); err == nil {
			return strings.TrimSpace(unq)
		}
	}
	return s
}

func extractToolContext(toolCalls []struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}) (string, []string) {
	var questions []string
	var edited []string
	for _, tc := range toolCalls {
		switch tc.Name {
		case "edit_file", "write_file", "replace_file_content", "multi_replace_file_content", "write_to_file":
			p := unquoteArg(tc.Args["Path"])
			if p == "" {
				p = unquoteArg(tc.Args["TargetFile"])
			}
			if p != "" {
				edited = append(edited, p)
			}
		case "ask_question":
			var raw []byte
			if s, ok := tc.Args["questions"].(string); ok {
				raw = []byte(s)
			} else {
				var err error
				raw, err = json.Marshal(tc.Args["questions"])
				if err != nil {
					continue
				}
			}
			var qs []struct {
				Question string   `json:"question"`
				Options  []string `json:"options"`
			}
			if err := json.Unmarshal(raw, &qs); err != nil {
				continue
			}
			for _, q := range qs {
				text := strings.TrimSpace(q.Question)
				if text == "" {
					continue
				}
				if len(q.Options) > 0 {
					text += " (" + strings.Join(q.Options, " / ") + ")"
				}
				questions = append(questions, text)
			}
		}
	}
	return strings.Join(questions, "\n"), edited
}

// HistoryFromSnapshot also reports whether the latest user input already has a model response.
func HistoryFromSnapshot(snapshot string, currentPrompt string) ([]checker.Message, bool, error) {
	history := []checker.Message{}
	cleanedCurrent := CleanUserPrompt(currentPrompt)
	unansweredUser := false
	currentUser := false
	continuation := false
	hasAssistantInTurn := false
	var turnEdited []string

	for _, line := range strings.Split(snapshot, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var row struct {
			Type      string `json:"type"`
			Source    string `json:"source"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal([]byte(trimmed), &row); err != nil {
			return nil, false, fmt.Errorf("failed to parse transcript line: %w", err)
		}
		switch row.Type {
		case "USER_INPUT":
			history = hook.FlushTurnEdits(history, hasAssistantInTurn, turnEdited)
			hasAssistantInTurn = false
			turnEdited = nil
			unansweredUser = false
			prompt := CleanUserPrompt(row.Content)
			currentUser = prompt != "" && prompt == cleanedCurrent
			continuation = false
			if prompt == "" {
				continue
			}
			history = append(history, checker.Message{Role: "user", Content: hook.TruncateContent(prompt, hook.MaxMessageRunes)})
			unansweredUser = true
		case "PLANNER_RESPONSE":
			unansweredUser = false
			continuation = currentUser
			questionText, edited := extractToolContext(row.ToolCalls)
			turnEdited = append(turnEdited, edited...)
			content := strings.TrimSpace(row.Content)
			if questionText != "" {
				if content != "" {
					content += "\n" + questionText
				} else {
					content = questionText
				}
			}
			if content != "" {
				history = append(history, checker.Message{Role: "assistant", Content: hook.TruncateContent(content, hook.MaxMessageRunes)})
				hasAssistantInTurn = true
			}
		case "ERROR_MESSAGE":
			hasAssistantInTurn = false
			turnEdited = nil
			if unansweredUser {
				history = history[:len(history)-1]
				unansweredUser = false
			}
		}
	}
	if unansweredUser && currentUser && len(history) > 0 {
		history = history[:len(history)-1]
	}
	history = hook.FlushTurnEdits(history, hasAssistantInTurn, turnEdited)
	return hook.TrimHistory(history), continuation, nil
}

func Run(stdin io.Reader, stdout io.Writer, rubric checker.Rubric, cfg checker.Config, client *http.Client) {
	response := map[string]any{}

	err := func() error {
		var event Event
		if err := json.NewDecoder(stdin).Decode(&event); err != nil {
			return err
		}
		if event.LastUserInput == "" {
			return errors.New("missing lastUserInput in hook event")
		}

		prompt := CleanUserPrompt(event.LastUserInput)
		if prompt == "" {
			return errors.New("empty user prompt after extraction")
		}

		var historyTurns []checker.Message
		var transcriptBytes int64
		if event.TranscriptPath != "" {
			snapshotBytes, err := os.ReadFile(event.TranscriptPath)
			if err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("failed to read transcript: %w", err)
				}
			} else {
				transcriptBytes = int64(len(snapshotBytes))
				h, continuation, err := HistoryFromSnapshot(string(snapshotBytes), prompt)
				if err != nil {
					return err
				}
				if continuation {
					return nil
				}
				historyTurns = h
			}
		}

		evalPrompt := prompt
		if imageCount := CountAttachedImages(event.LastUserInput); imageCount > 0 {
			evalPrompt = fmt.Sprintf("%s\n\n[Attached %d image(s)]", prompt, imageCount)
		}

		result, err := checker.Check(evalPrompt, historyTurns, rubric, cfg, client)
		if err != nil {
			if checker.IsNetworkError(err) {
				return nil
			}
			return err
		}

		_ = history.Append(history.Record{
			Timestamp:       time.Now().UTC(),
			SessionID:       event.ConversationID,
			TranscriptBytes: &transcriptBytes,
			Agent:           "agy",
			Prompt:          prompt,
			Pass:            result.Pass,
			Scores:          result.Scores,
			Failed:          result.Failed,
			Clarifications:  result.Clarifications,
			ElapsedMS:       result.ElapsedMS,
			TranscriptPath:  event.TranscriptPath,
		}, "")

		if !result.Pass {
			response = blockResponse(hook.FormatBlockReason(result.Scores, rubric, cfg.Threshold))
		}
		return nil
	}()

	if err != nil {
		WriteError(stdout, err)
		return
	}

	out, _ := json.Marshal(response)
	fmt.Fprintln(stdout, string(out))
}

func blockResponse(reason string) map[string]any {
	return map[string]any{
		"terminationBehavior": "terminate",
		"injectSteps": []map[string]any{
			{"errorMessage": map[string]any{"userMessage": reason}},
		},
	}
}

// WriteError writes a terminating hook response for a prompt check failure.
func WriteError(stdout io.Writer, err error) {
	_ = json.NewEncoder(stdout).Encode(blockResponse("Prompt check failed: " + err.Error()))
}
