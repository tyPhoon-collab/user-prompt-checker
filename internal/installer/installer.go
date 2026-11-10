package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ConfigPaths struct {
	AGY   string
	Codex string
}

func DefaultConfigPaths() (ConfigPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ConfigPaths{}, fmt.Errorf("failed to get user home directory: %w", err)
	}
	return ConfigPaths{
		AGY:   filepath.Join(home, ".gemini", "config", "hooks.json"),
		Codex: filepath.Join(home, ".codex", "hooks.json"),
	}, nil
}

func Install(agent string, binaryPath string, targetFile string) error {
	absBinary, err := filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to resolve binary path: %w", err)
	}

	if targetFile == "" {
		paths, err := DefaultConfigPaths()
		if err != nil {
			return err
		}
		switch agent {
		case "agy":
			targetFile = paths.AGY
		case "codex":
			targetFile = paths.Codex
		default:
			return fmt.Errorf("unknown agent: %s (supported: agy, codex)", agent)
		}
	}

	if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var root map[string]any
	if data, err := os.ReadFile(targetFile); err == nil {
		_ = os.WriteFile(targetFile+".bak", data, 0o600)
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("failed to parse existing %s: %w", targetFile, err)
		}
	} else if os.IsNotExist(err) {
		root = make(map[string]any)
	} else {
		return fmt.Errorf("failed to read %s: %w", targetFile, err)
	}

	if root == nil {
		root = make(map[string]any)
	}

	switch agent {
	case "agy":
		root["user-prompt-checker"] = map[string]any{
			"PreInvocation": []map[string]any{
				{
					"type":    "command",
					"command": absBinary + " hook agy",
					"timeout": 15,
				},
			},
		}
	case "codex":
		var hooksMap map[string]any
		if existingHooks, ok := root["hooks"].(map[string]any); ok {
			hooksMap = existingHooks
		} else {
			hooksMap = make(map[string]any)
			root["hooks"] = hooksMap
		}

		entry := map[string]any{
			"hooks": []map[string]any{
				{
					"type":          "command",
					"command":       absBinary + " hook codex",
					"timeout":       15,
					"statusMessage": "Checking user prompt with local d1",
				},
			},
		}

		var existingList []any
		if rawList, ok := hooksMap["UserPromptSubmit"].([]any); ok {
			existingList = rawList
		}

		found := false
		newList := make([]any, 0, len(existingList)+1)
		for _, item := range existingList {
			if updateCodexCheckerHooks(item, entry["hooks"].([]map[string]any)[0]) {
				found = true
			}
			newList = append(newList, item)
		}
		if !found {
			newList = append(newList, entry)
		}
		hooksMap["UserPromptSubmit"] = newList
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	out = append(out, '\n')

	if err := os.WriteFile(targetFile, out, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", targetFile, err)
	}

	return nil
}

func Uninstall(agent string, targetFile string) error {
	if targetFile == "" {
		paths, err := DefaultConfigPaths()
		if err != nil {
			return err
		}
		switch agent {
		case "agy":
			targetFile = paths.AGY
		case "codex":
			targetFile = paths.Codex
		default:
			return fmt.Errorf("unknown agent: %s (supported: agy, codex)", agent)
		}
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", targetFile, err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to parse existing %s: %w", targetFile, err)
	}

	_ = os.WriteFile(targetFile+".bak", data, 0o600)

	switch agent {
	case "agy":
		delete(root, "user-prompt-checker")
	case "codex":
		if hooksMap, ok := root["hooks"].(map[string]any); ok {
			if rawList, ok := hooksMap["UserPromptSubmit"].([]any); ok {
				var remaining []any
				for _, item := range rawList {
					if updateCodexCheckerHooks(item, nil) {
						if len(item.(map[string]any)["hooks"].([]any)) == 0 {
							continue
						}
					}
					remaining = append(remaining, item)
				}
				if len(remaining) > 0 {
					hooksMap["UserPromptSubmit"] = remaining
				} else {
					delete(hooksMap, "UserPromptSubmit")
				}
			}
			if len(hooksMap) == 0 {
				delete(root, "hooks")
			}
		}
	default:
		return errors.New("unknown agent: " + agent)
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	out = append(out, '\n')

	return os.WriteFile(targetFile, out, 0o600)
}

func updateCodexCheckerHooks(item any, replacement map[string]any) bool {
	m, ok := item.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	found := false
	remaining := make([]any, 0, len(hooks))
	for _, h := range hooks {
		if hm, ok := h.(map[string]any); ok {
			if cmd, ok := hm["command"].(string); ok && (containsCheckerCommand(cmd) ||
				(hm["statusMessage"] == "Checking user prompt with local d1" && strings.HasSuffix(cmd, " hook codex"))) {
				found = true
				if replacement != nil {
					remaining = append(remaining, replacement)
				}
				continue
			}
		}
		remaining = append(remaining, h)
	}
	if found {
		m["hooks"] = remaining
	}
	return found
}

func containsCheckerCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	return len(fields) == 3 && filepath.Base(fields[0]) == "user-prompt-checker" && fields[1] == "hook" && fields[2] == "codex"
}
