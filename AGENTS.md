構成・仕様は `README.md`、用語は `GLOSSARY.md`、設計判断は `docs/adr/` を参照し、新規・変更時はこれらも更新する。

- **タスク実行**: `just` レシピ（`test`, `lint`, `evaluate`, `check`, `gain`）を使う。`evaluate` と `check` はローカル Ollama（`d1:2b` / `localhost:11434`）へ接続する。
- **ケース追加**: `cases.yaml` には実セッションの固有情報や個人情報を入れず、抽象化した汎用的な例を追加する。
- **判定調整**: `internal/checker/rubric.yaml`（`//go:embed` のため要再ビルド）や判定ロジック変更時は、`just test`・`just lint`・`just evaluate` を実行して `results.json` を更新し、リグレッションを確認する。個別プロンプトへの過適合なルールは書かない。
- **運用調査**: 誤検知やブロック傾向の確認には `just gain`（または `./user-prompt-checker gain --format json`）を使う。
