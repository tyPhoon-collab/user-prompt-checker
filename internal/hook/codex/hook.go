package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/hook"
)

const maxHistoryMessages = 10

var skillTagRegex = regexp.MustCompile(`(?s)<skill>.*?</skill>`)

func stripSkillTags(s string) string {
	return strings.TrimSpace(skillTagRegex.ReplaceAllString(s, ""))
}

func IsQuestionReplyPrompt(prompt string) bool {
	trimmed := strings.TrimSpace(prompt)
	return strings.HasPrefix(trimmed, "<send_user_message_question_reply>") &&
		strings.HasSuffix(trimmed, "</send_user_message_question_reply>")
}

func CountAttachedImages(snapshot string, currentTurnID string) int {
	count := 0
	for _, line := range strings.Split(snapshot, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
				Metadata struct {
					TurnID string `json:"turn_id"`
				} `json:"internal_chat_message_metadata_passthrough"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(trimmed), &row); err != nil {
			continue
		}
		if row.Type != "response_item" || row.Payload.Role != "user" {
			continue
		}
		if row.Payload.Metadata.TurnID != currentTurnID {
			continue
		}
		for _, item := range row.Payload.Content {
			if item.Type == "input_image" {
				count++
			}
		}
	}
	return count
}

func HistoryFromSnapshot(snapshot string, currentTurnID string) ([]checker.Message, []string, error) {
	history := []checker.Message{}
	var warnings []string
	warnedProvenance := false

	// shortcut: private JSONL schema for Codex CLI 0.166.0; revisit after CLI updates.
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string `json:"type"`
				Role      string `json:"role"`
				Phase     string `json:"phase"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Content   []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Metadata struct {
					TurnID              string          `json:"turn_id"`
					ContentItemMetadata json.RawMessage `json:"content_item_metadata"`
				} `json:"internal_chat_message_metadata_passthrough"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, nil, err
		}
		// Compaction records do not replace the original messages in the JSONL log.
		if row.Type != "response_item" {
			continue
		}
		p := row.Payload
		if p.Metadata.TurnID == currentTurnID {
			continue
		}
		if p.Type == "function_call" && p.Name == "request_user_input_async" {
			var args struct {
				Questions []struct {
					Title    string   `json:"title"`
					Question string   `json:"question"`
					Options  []string `json:"options"`
				} `json:"questions"`
			}
			if err := json.Unmarshal([]byte(p.Arguments), &args); err == nil {
				var parts []string
				for _, q := range args.Questions {
					text := strings.TrimSpace(q.Title)
					if text == "" {
						text = strings.TrimSpace(q.Question)
					}
					if text != "" {
						if len(q.Options) > 0 {
							text += " (" + strings.Join(q.Options, " / ") + ")"
						}
						parts = append(parts, text)
					}
				}
				if len(parts) > 0 {
					history = append(history, checker.Message{Role: "assistant", Content: strings.Join(parts, "\n")})
				}
			}
			continue
		}
		if p.Type != "message" || (p.Role != "user" && p.Role != "assistant") {
			continue
		}
		var texts []string
		if p.Role == "user" {
			var itemMeta []struct {
				Provenance struct {
					Type string `json:"type"`
				} `json:"provenance"`
			}
			hasProvenance := false
			if len(p.Metadata.ContentItemMetadata) > 0 {
				if err := json.Unmarshal(p.Metadata.ContentItemMetadata, &itemMeta); err == nil && len(itemMeta) == len(p.Content) {
					hasProvenance = true
				}
			}
			if !hasProvenance && !warnedProvenance && len(p.Content) > 0 {
				warnings = append(warnings, "user provenance metadata missing in transcript; fallen back to role-based extraction")
				warnedProvenance = true
			}
			for i, item := range p.Content {
				if item.Type != "input_text" && item.Type != "output_text" {
					continue
				}
				if hasProvenance && itemMeta[i].Provenance.Type != "user" {
					continue
				}
				texts = append(texts, item.Text)
			}
		} else {
			if p.Phase != "final_answer" {
				continue
			}
			for _, item := range p.Content {
				if item.Type == "input_text" || item.Type == "output_text" {
					texts = append(texts, item.Text)
				}
			}
		}
		joined := stripSkillTags(strings.Join(texts, "\n"))
		if joined != "" {
			history = append(history, checker.Message{Role: p.Role, Content: joined})
		}
	}
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}
	return history, warnings, nil
}

func Run(stdin io.Reader, stdout io.Writer, rubric checker.Rubric, cfg checker.Config, client *http.Client) {
	response := map[string]any{}

	err := func() error {
		var event map[string]any
		if err := json.NewDecoder(stdin).Decode(&event); err != nil {
			return err
		}
		if event["hook_event_name"] != "UserPromptSubmit" {
			return errors.New("hook only supports UserPromptSubmit")
		}

		var snapshot string
		transcriptPath, _ := event["transcript_path"].(string)
		if transcriptPath != "" {
			snapshotBytes, err := os.ReadFile(transcriptPath)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				snapshot = string(snapshotBytes)
			}
		}
		turnID, _ := event["turn_id"].(string)
		sessionID, _ := event["session_id"].(string)
		prompt, ok := event["prompt"].(string)
		if !ok {
			return errors.New("missing prompt in hook event")
		}
		if IsQuestionReplyPrompt(prompt) {
			return nil
		}
		historyTurns, warnings, err := HistoryFromSnapshot(snapshot, turnID)
		if err != nil {
			return err
		}
		evalPrompt := prompt
		if imageCount := CountAttachedImages(snapshot, turnID); imageCount > 0 {
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
			SessionID:       sessionID,
			TurnID:          turnID,
			TranscriptBytes: new(int64(len(snapshot))),
			Agent:           "codex",
			Prompt:          evalPrompt,
			Pass:            result.Pass,
			Scores:          result.Scores,
			Failed:          result.Failed,
			Clarifications:  result.Clarifications,
			ElapsedMS:       result.ElapsedMS,
			TranscriptPath:  transcriptPath,
		}, "")

		if !result.Pass {
			response = blockResponse(hook.FormatBlockReason(result.Scores, rubric, cfg.Threshold))
		} else if len(warnings) > 0 {
			response = map[string]any{
				"hookSpecificOutput": map[string]any{
					"hookEventName":     "UserPromptSubmit",
					"additionalContext": "[user-prompt-checker] Warning: " + strings.Join(warnings, "; "),
				},
			}
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
	return map[string]any{"decision": "block", "reason": reason}
}

// WriteError writes a blocking hook response for a prompt check failure.
func WriteError(stdout io.Writer, err error) {
	_ = json.NewEncoder(stdout).Encode(blockResponse("Prompt check failed: " + err.Error()))
}
