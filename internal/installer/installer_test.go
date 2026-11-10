package installer_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"user-prompt-checker/internal/installer"
)

func TestMixedCodexHookGroupIsPreserved(t *testing.T) {
	for _, uninstall := range []bool{false, true} {
		t.Run(fmt.Sprint(uninstall), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "hooks.json")
			input := `{"hooks":{"UserPromptSubmit":[{"matcher":"special","hooks":[{"type":"command","command":"/old/user-prompt-checker hook codex"},{"type":"command","command":"other-linter"},{"type":"command","command":"other-tool hook codex"},{"type":"command","command":"echo user-prompt-checker"}]}]}}`
			if err := os.WriteFile(target, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if uninstall {
				err = installer.Uninstall("codex", target)
			} else {
				err = installer.Install("codex", "/new/user-prompt-checker", target)
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]any
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatal(err)
			}
			groups := root["hooks"].(map[string]any)["UserPromptSubmit"].([]any)
			group := groups[0].(map[string]any)
			if group["matcher"] != "special" {
				t.Fatalf("matcher lost: %s", data)
			}
			hooks := group["hooks"].([]any)
			want := 4
			if uninstall {
				want = 3
			}
			if len(hooks) != want {
				t.Fatalf("unexpected hooks: %s", data)
			}
			foundOther := false
			for _, h := range hooks {
				if h.(map[string]any)["command"] == "other-linter" {
					foundOther = true
				}
			}
			if !foundOther {
				t.Fatalf("other hook lost: %s", data)
			}
		})
	}
}

func TestInstallAndUninstallAGY(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "hooks.json")
	binPath := "/usr/local/bin/user-prompt-checker"

	// 1. 新規インストール
	if err := installer.Install("agy", binPath, targetFile); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("failed to unmarshal written file: %v", err)
	}

	agyEntry, ok := root["user-prompt-checker"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'user-prompt-checker' key in config, got %v", root)
	}
	pre, ok := agyEntry["PreInvocation"].([]any)
	if !ok || len(pre) == 0 {
		t.Fatalf("expected 'PreInvocation' array in config, got %v", agyEntry)
	}
	cmdMap, ok := pre[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map in PreInvocation[0], got %v", pre[0])
	}
	if cmdMap["command"] != binPath+" hook agy" {
		t.Errorf("expected command %q, got %q", binPath+" hook agy", cmdMap["command"])
	}

	// 2. 既存の設定（他のキー）を壊さずに保持・更新できるか
	root["other-tool"] = map[string]any{"enabled": true}
	modifiedData, _ := json.MarshalIndent(root, "", "  ")
	if err := os.WriteFile(targetFile, modifiedData, 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	newBin := "/opt/bin/user-prompt-checker"
	if err := installer.Install("agy", newBin, targetFile); err != nil {
		t.Fatalf("re-install failed: %v", err)
	}

	// .bak が作られているか
	if _, err := os.Stat(targetFile + ".bak"); err != nil {
		t.Fatalf("backup file %s.bak was not created", targetFile)
	}

	data, _ = os.ReadFile(targetFile)
	var reloaded map[string]any
	_ = json.Unmarshal(data, &reloaded)
	if _, ok := reloaded["other-tool"]; !ok {
		t.Errorf("other-tool key was lost during re-install")
	}

	// 3. アンインストール
	if err := installer.Uninstall("agy", targetFile); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	data, _ = os.ReadFile(targetFile)
	var afterUninstall map[string]any
	_ = json.Unmarshal(data, &afterUninstall)
	if _, ok := afterUninstall["user-prompt-checker"]; ok {
		t.Errorf("user-prompt-checker key was not removed")
	}
	if _, ok := afterUninstall["other-tool"]; !ok {
		t.Errorf("other-tool key should be preserved after uninstall")
	}
}

func TestInstallAndUninstallCodex(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "hooks.json")
	binPath := "/usr/local/bin/user-prompt-checker"

	// 1. 新規インストール
	if err := installer.Install("codex", binPath, targetFile); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("failed to unmarshal written file: %v", err)
	}

	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'hooks' map in config, got %v", root)
	}
	ups, ok := hooks["UserPromptSubmit"].([]any)
	if !ok || len(ups) == 0 {
		t.Fatalf("expected 'UserPromptSubmit' array, got %v", hooks)
	}

	// 2. 既存の別ツールフックがある状態で再度インストール・更新（共存と冪等性の検証）
	existingOtherHook := map[string]any{
		"hooks": []map[string]any{
			{"type": "command", "command": "other-linter", "timeout": 5},
		},
	}
	root["hooks"].(map[string]any)["UserPromptSubmit"] = []any{existingOtherHook}
	modifiedData, _ := json.MarshalIndent(root, "", "  ")
	if err := os.WriteFile(targetFile, modifiedData, 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	// 1回目：追加
	newBin := "/opt/bin/user-prompt-checker"
	if err := installer.Install("codex", newBin, targetFile); err != nil {
		t.Fatalf("re-install failed: %v", err)
	}

	data, _ = os.ReadFile(targetFile)
	_ = json.Unmarshal(data, &root)
	upsList := root["hooks"].(map[string]any)["UserPromptSubmit"].([]any)
	if len(upsList) != 2 {
		t.Fatalf("expected 2 hooks (other + checker), got %d", len(upsList))
	}

	// 2回目：再実行しても2件のままであること（冪等性）
	if err := installer.Install("codex", newBin, targetFile); err != nil {
		t.Fatalf("idempotent install failed: %v", err)
	}
	data, _ = os.ReadFile(targetFile)
	_ = json.Unmarshal(data, &root)
	upsList = root["hooks"].(map[string]any)["UserPromptSubmit"].([]any)
	if len(upsList) != 2 {
		t.Fatalf("expected 2 hooks after repeated install, got %d", len(upsList))
	}

	// 3. アンインストール時、other-linter は残り checker のみ消えること
	if err := installer.Uninstall("codex", targetFile); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	data, _ = os.ReadFile(targetFile)
	var afterUninstall map[string]any
	_ = json.Unmarshal(data, &afterUninstall)
	hooksAfter, ok := afterUninstall["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks map should still exist because other hook remains")
	}
	upsAfter, ok := hooksAfter["UserPromptSubmit"].([]any)
	if !ok || len(upsAfter) != 1 {
		t.Fatalf("expected 1 hook remaining, got %v", upsAfter)
	}

	// 4. 残った other-linter も消した空状態での Uninstall で hooks キー自体が消えること
	hooksAfter["UserPromptSubmit"] = []any{}
	emptyData, _ := json.MarshalIndent(afterUninstall, "", "  ")
	_ = os.WriteFile(targetFile, emptyData, 0o600)
	if err := installer.Uninstall("codex", targetFile); err != nil {
		t.Fatalf("final uninstall failed: %v", err)
	}
	data, _ = os.ReadFile(targetFile)
	var finalRoot map[string]any
	_ = json.Unmarshal(data, &finalRoot)
	if _, ok := finalRoot["hooks"]; ok {
		t.Errorf("hooks key should be deleted when completely empty")
	}
}
