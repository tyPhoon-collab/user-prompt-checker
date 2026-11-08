---
status: accepted
---

# On-Demand Prompt Quality Gain Analysis

Agent lifecycle hooks operate silently on success, depriving users of positive feedback and visibility into prompt improvements over time. We introduce an on-demand `user-prompt-checker gain` command (and `just gain` recipe) to measure score improvements when users revise blocked prompts and highlight high-performing prompts without injecting noise into active chat sessions.
