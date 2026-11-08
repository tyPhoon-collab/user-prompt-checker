---
status: accepted
---

# Unified Capture Storage under XDG Data Home

Hook evaluation logs were previously split across agent-specific directories (`~/.gemini/...` and `~/.codex/...`), making cross-agent metrics aggregation difficult. We now store all evaluation logs in `$XDG_DATA_HOME/user-prompt-checker/` (defaulting to `~/.local/share/user-prompt-checker/`) to adhere to modern CLI standards and enable unified analysis across both AGY and Codex.
