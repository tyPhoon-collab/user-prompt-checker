package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"user-prompt-checker/internal/checker"
	"user-prompt-checker/internal/history"
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

func HistoryFromSnapshot(snapshot string, currentTurnID string) ([]checker.Message, error) {
	history := []checker.Message{}
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
			return nil, err
		}
		if row.Type == "compacted" {
			return nil, errors.New("compacted conversation transcripts are not supported")
		}
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
			if len(p.Metadata.ContentItemMetadata) == 0 {
				return nil, errors.New("cannot identify user provenance in transcript")
			}
			if err := json.Unmarshal(p.Metadata.ContentItemMetadata, &itemMeta); err != nil || len(itemMeta) != len(p.Content) {
				return nil, errors.New("cannot identify user provenance in transcript")
			}
			for i, item := range p.Content {
				if itemMeta[i].Provenance.Type == "user" && (item.Type == "input_text" || item.Type == "output_text") {
					texts = append(texts, item.Text)
				}
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
		joined := strings.Join(texts, "\n")
		if strings.TrimSpace(joined) != "" {
			history = append(history, checker.Message{Role: p.Role, Content: joined})
		}
	}
	if len(history) > 6 {
		history = history[len(history)-6:]
	}
	return history, nil
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

		transcriptPath, _ := event["transcript_path"].(string)
		if transcriptPath == "" {
			return errors.New("missing transcript_path in hook event")
		}
		snapshotBytes, err := os.ReadFile(transcriptPath)
		if err != nil {
			return err
		}
		snapshot := string(snapshotBytes)
		turnID, ok := event["turn_id"].(string)
		if !ok || turnID == "" {
			return errors.New("missing turn_id in hook event")
		}
		prompt, ok := event["prompt"].(string)
		if !ok {
			return errors.New("missing prompt in hook event")
		}
		if IsQuestionReplyPrompt(prompt) {
			return nil
		}
		historyTurns, err := HistoryFromSnapshot(snapshot, turnID)
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
			Timestamp:      time.Now().UTC(),
			SessionID:      turnID,
			Agent:          "codex",
			Prompt:         evalPrompt,
			Pass:           result.Pass,
			Scores:         result.Scores,
			Failed:         result.Failed,
			Clarifications: result.Clarifications,
			ElapsedMS:      result.ElapsedMS,
			TranscriptPath: transcriptPath,
		}, "")

		if !result.Pass {
			response = map[string]any{
				"decision": "block",
				"reason":   FormatBlockReason(result.Scores, rubric, cfg.Threshold),
			}
		}
		return nil
	}()

	if err != nil {
		response = map[string]any{
			"decision": "block",
			"reason":   "Prompt check failed: " + err.Error(),
		}
	}

	out, _ := json.Marshal(response)
	fmt.Fprintln(stdout, string(out))
}
