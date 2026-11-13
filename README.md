# user-prompt-checker

A standalone Go CLI and agent lifecycle hook (supporting both OpenAI Codex and Google Antigravity / AGY) that checks user prompts and optional conversation history against three criteria in a single Ollama `d1:2b` `/v1/systemone` API call.
The default rubric is embedded directly into the binary.

## Build and Install

Toolchains are managed via `mise` (`mise.toml`), and project tasks are managed via `just` (`justfile`).
Ensure Ollama is running at `http://localhost:11434` with `d1:2b` available:

```sh
# Verify Ollama status and d1:2b model availability:
just ollama::status
# Pull d1:2b if not yet installed:
just ollama::pull
```

```sh
# Build and test using just:
just build
just test

# Or using standard Go commands:
go build -o user-prompt-checker ./cmd/user-prompt-checker
# Or install to GOBIN / GOPATH/bin:
go install ./cmd/user-prompt-checker
```

Run `just --list` to view all available tasks.

## Usage

```sh
# Quick check via CLI (or `just check '<prompt>'`):
./user-prompt-checker check 'Change the background color of the submit button on the login screen to #0066cc.'
./user-prompt-checker check 'Proceed with that.' --history history.json

# Run evaluation benchmark:
just evaluate
# or: ./user-prompt-checker evaluate

# Analyze prompt score improvements and quality history (interactive TUI dashboard on terminal):
just gain
# or: ./user-prompt-checker gain [--format text|json]
```

`check` returns the three scores, pass/block decision, failed criteria, clarification prompts, and elapsed time as JSON.
The exit code is `0` when the check completes (even if `pass` is `false`), `1` on network or input errors, and `2` on invalid CLI flags.
Check the `pass` field in the JSON output to determine whether the prompt passed.

New hook history records keep conversation and turn identifiers separately and record the transcript byte boundary at evaluation time.
The gain dashboard reads context only within that boundary. Older records remain available for score analysis, but their conversation context is omitted because the original boundary is unknown.

`evaluate` runs all cases in `cases.yaml` in fixed order, prints per-case scores and summary metrics, and writes the full report (inputs, rubric, raw model responses, latency) to `results.json`.
Specify `--output <path>` to save to another file or `--threshold <float>` to test a different threshold:

```sh
./user-prompt-checker --threshold 0.7 evaluate --output results-070.json
```

`history.json` is a chronological array of prior messages (excluding the current prompt):

```json
[
  {"role": "user", "content": "I want to make the submit button on the login screen blue."},
  {"role": "assistant", "content": "How about changing the background color to #0066cc?"}
]
```

## How It Works

### Architecture

```mermaid
flowchart LR
    subgraph Codex["Codex CLI Environment"]
        User["User Prompt"] --> HookEvent["UserPromptSubmit Event"]
        Transcript["Session Transcript (.jsonl)"]
    end

    subgraph UPC["user-prompt-checker"]
        CLI["CLI / Hook Runner"]
        Parser["Transcript Parser"]
        Rubric["Embedded Rubric (3 criteria)"]
        Engine["Score & Decision Engine"]
    end

    subgraph Ollama["Local Ollama Service"]
        Model["d1:2b (/v1/systemone)"]
    end

    HookEvent --> CLI
    Transcript --> Parser
    CLI --> Parser
    Parser --> Engine
    Rubric --> Engine
    Engine --> Model
    Model --> Engine
    Engine --> History["Prompt Records (monthly history JSONL)"]
    Engine --> HookOutput["Hook Response (Allow or Block)"]
```

### Hook Evaluation Flow

```mermaid
flowchart TD
    Start["Receive UserPromptSubmit Event"] --> ReadTranscript["Read transcript_path (.jsonl)"]
    ReadTranscript -- "Read error (except missing file)" --> BlockError["Block: return error reason"]
    ReadTranscript -- "Read or missing file" --> IsReply{"Structured question reply?"}
    IsReply -- "Yes" --> AllowSkip["Allow: return empty JSON {}\n(Skip model check)"]
    IsReply -- "No" --> ParseHistory{"Parse history\n(User, Assistant, Tool questions)"}
    ParseHistory -- "Malformed" --> BlockError
    ParseHistory -- "OK (up to last 10)" --> CallOllama["Call Ollama d1:2b (/v1/systemone)\n10s timeout"]

    CallOllama --> NetResult{"HTTP call status"}
    NetResult -- "Connection Refused / Timeout" --> AllowNet["Allow: return empty JSON {}\n(No record written)"]
    NetResult -- "Other Error" --> BlockError
    NetResult -- "Evaluation complete" --> SaveHistory["Append Prompt Record\n(monthly history JSONL)"]
    SaveHistory --> EvaluateScores{"All 3 scores >= threshold (0.6)?"}

    EvaluateScores -- "Yes (All pass)" --> AllowPass["Allow: return empty JSON {}"]
    EvaluateScores -- "No (Any below 0.6)" --> BlockDecision["Block: return decision block\nPlain-text clarifications & metrics"]
```

## Evaluation Criteria

`internal/checker/rubric.json` (embedded by default, overrideable via `--rubric`) defines three independent criteria evaluated in Japanese:

| Criterion | What it checks |
| --- | --- |
| `intent_clear` | 単なる名詞句ではなく、行動の要求、質問、または会話の返答を表現しているか |
| `context_sufficient` | プロンプト単体または直前の会話を踏まえて、対象と期待する結果を特定できるか |
| `consistent` | プロンプトと会話内の要求が論理的に両立可能か（指示の訂正や変更は上書きとみなす） |

User input can be in any language supported by `d1:2b` (current test cases cover Japanese and English; broader multilingual accuracy has not been independently verified).
Each score is the raw `noul` value returned by `/v1/systemone`.
If any score is below `--threshold` (default `0.6`), the prompt is marked `pass: false`.
A score equal to the threshold passes.

## Evaluation Scope and Limitations

`cases.yaml` includes explicit requests, missing references, bare noun phrases, vague quality requests, conflicting constraints, harmless typos, option selection, requirement corrections, exploratory brainstorming, prompt injection attempts, and real follow-up prompts captured from Codex sessions.

Expected values in `cases.yaml` are provisional hypotheses rather than independently calibrated ground truth.
When changing the rubric or threshold, validate against held-out examples rather than treating `cases.yaml` match counts as independent accuracy.
Reported latency includes HTTP round-trip time with the model warm in memory; cold loads and long transcripts are not guaranteed to match.

Model specification: [Ollama d1:2b](https://ollama.com/library/d1:2b)

## Agent Hook Integration

The `hook <agent>` subcommand adapts to the lifecycle hook protocol of the specified agent (`codex` or `agy`). The `<agent>` argument is required.

When evaluation completes, either hook appends a Prompt Record to `$XDG_DATA_HOME/user-prompt-checker/history-YYYY-MM.jsonl`, defaulting to `~/.local/share/user-prompt-checker/history-YYYY-MM.jsonl` when `XDG_DATA_HOME` is unset or empty. New directories are created with `0700` permissions and new files with `0600` permissions.
Records retain the transcript path and byte boundary for context lookup, without copying the transcript itself.
Skipped checks and processing errors write no record or skip reason. History write failures do not change the hook response.

You can automatically register or remove the hooks using `install` / `uninstall`:
```sh
# Automatic setup (creates .bak backup and safely merges into existing hooks.json):
just install agy      # or: ./user-prompt-checker install agy
just install codex    # or: ./user-prompt-checker install codex

# To remove:
just uninstall agy    # or: ./user-prompt-checker uninstall agy
just uninstall codex  # or: ./user-prompt-checker uninstall codex
```

### 1. Codex CLI Hook Integration (`hook codex`)

The `hook codex` command reads a `UserPromptSubmit` JSON event from stdin and reads `transcript_path` to extract up to the last 10 messages (`user` messages with `user` provenance, `assistant` `final_answer` messages, and `request_user_input_async` tool questions from prior turns).
Compaction records are skipped; context is extracted from the original messages retained in the JSONL log, including messages before compaction. If those messages are absent, only the available messages are used.
Structured tool replies (`<send_user_message_question_reply>...</send_user_message_question_reply>`) automatically skip model evaluation and pass through.

- **When all scores >= 0.6**: outputs `{}` with no notification.
- **When any score < 0.6**: outputs `{"decision": "block", "reason": "..."}` containing plain-text clarification bullets for the failed criteria followed by all three scores and the threshold (no ANSI escape sequences, as Codex CLI `0.166.0` renders raw escape codes).
- **When Ollama is unreachable or times out** (default hook timeout: 10s): skips the check and outputs `{}` so normal work is not blocked.
- **On transcript format or non-network errors**: returns `{"decision": "block"}` with the error reason.

#### Installing in `~/.codex/hooks.json`

To enable the hook in your normal Codex environment, build the binary and register `<path-to-binary>/user-prompt-checker hook codex` under `UserPromptSubmit` in `~/.codex/hooks.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/user-prompt-checker hook codex",
            "timeout": 15,
            "statusMessage": "Checking user prompt with local d1"
          }
        ]
      }
    ]
  }
}
```

After updating `hooks.json`, run `/hooks` inside Codex CLI to review and trust the hook.
Transcript parsing relies on the private JSONL format observed in Codex CLI `0.166.0`; image inputs and future CLI schema changes require separate verification.
Hook protocol reference: [OpenAI Hooks Documentation](https://learn.chatgpt.com/docs/hooks)

### 2. Antigravity Hook Integration (`hook agy`)

The `hook agy` command integrates with Antigravity as a `PreInvocation` lifecycle hook.
It receives the hook payload via stdin, extracts the prompt from `lastUserInput` (stripping enclosing `<USER_REQUEST>` wrappers if present), and parses prior `USER_INPUT` and `PLANNER_RESPONSE` turns from `transcriptPath`.
If the latest transcript `USER_INPUT` matches the prompt and already has a `PLANNER_RESPONSE`, the hook returns `{}` without evaluating or recording it again. A new `USER_INPUT` is evaluated even when its text is unchanged.

- **When all scores >= 0.6**: outputs `{}` to allow the agent execution to proceed normally.
- **When any score < 0.6**: outputs:
  ```json
  {
    "terminationBehavior": "terminate",
    "injectSteps": [
      {
        "errorMessage": {
          "userMessage": "- 対象の指定、選択肢の選択、または期待する結果を具体化してください。\n\n意図の明確さ ...\nThreshold ..."
        }
      }
    ]
  }
  ```
  This halts execution before model invocation and displays the guidance message to the user.
- **When Ollama is unreachable or times out**: outputs `{}` and skips the check without blocking the user.

#### Installing in AGY (`hooks.json`)

To enable the hook in AGY, register it in your workspace `.agents/hooks.json` or global `~/.gemini/config/hooks.json`:

```json
{
  "user-prompt-checker": {
    "PreInvocation": [
      {
        "type": "command",
        "command": "/path/to/user-prompt-checker hook agy",
        "timeout": 15
      }
    ]
  }
}
```
