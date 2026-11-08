package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Record struct {
	Timestamp      time.Time          `json:"timestamp"`
	SessionID      string             `json:"session_id"`
	Agent          string             `json:"agent"`
	Prompt         string             `json:"prompt"`
	Pass           bool               `json:"pass"`
	Scores         map[string]float64 `json:"scores"`
	Failed         []string           `json:"failed"`
	Clarifications []string           `json:"clarifications"`
	ElapsedMS      float64            `json:"elapsed_ms"`
}

func DataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "user-prompt-checker"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "user-prompt-checker"), nil
}

func Append(rec Record, targetDir string) error {
	dir := targetDir
	if dir == "" {
		var err error
		dir, err = DataDir()
		if err != nil {
			return err
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}

	filename := fmt.Sprintf("history-%s.jsonl", rec.Timestamp.Format("2006-01"))
	filePath := filepath.Join(dir, filename)

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal history record: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("failed to append history record: %w", err)
	}

	return nil
}

func LoadAll(targetDir string) ([]Record, error) {
	dir := targetDir
	if dir == "" {
		var err error
		dir, err = DataDir()
		if err != nil {
			return nil, err
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var jsonlFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "history-") && strings.HasSuffix(entry.Name(), ".jsonl") {
			jsonlFiles = append(jsonlFiles, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(jsonlFiles)

	var records []Record
	for _, path := range jsonlFiles {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			var r Record
			if err := json.Unmarshal([]byte(text), &r); err == nil {
				records = append(records, r)
			}
		}
		_ = file.Close()
	}

	return records, nil
}
