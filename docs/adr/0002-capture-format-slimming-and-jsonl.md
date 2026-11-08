---
status: accepted
---

# Monthly JSONL Format for Slimmed Evaluation History

Capturing full conversation snapshots in individual JSON files caused rapid disk bloat (~1.5 MB per turn, exceeding 300 MB within days). We replace them with a monthly append-only `history-YYYY-MM.jsonl` file containing only essential evaluation metrics, reducing per-record size by roughly 99.9% (~400 bytes) and preventing filesystem exhaustion.
