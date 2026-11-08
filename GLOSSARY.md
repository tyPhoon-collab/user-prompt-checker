# user-prompt-checker

A standalone CLI and agent lifecycle hook that evaluates user prompt clarity, scope, and context sufficiency against an embedded rubric using a local LLM.

## Language

**Prompt Record**:
An immutable record capturing a single user prompt evaluation event, its rubric scores, pass/block decision, and timing.
_Avoid_: Capture, log entry, snapshot, event dump

**Prompt Gain**:
The measurable increase in rubric score achieved when a user revises and resubmits a previously blocked prompt within the same session.
_Avoid_: Score diff, delta, improvement rate, bonus

**Rubric**:
The canonical three-criterion evaluation standard (Intent Clarity, Scope Specificity, and Context Self-Sufficiency) against which prompts are scored.
_Avoid_: Rule, criteria list, spec, checklist

**Agent Hook**:
A lifecycle interceptor invoked by an AI coding agent (Codex or AGY) prior to LLM submission to validate the incoming user prompt.
_Avoid_: Plugin, middleware, filter, proxy
