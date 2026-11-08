# Submodule for Ollama environment tasks
mod ollama

# Default recipe: show available recipes
default:
    @just --list

# Build the user-prompt-checker binary
build:
    go build -o user-prompt-checker ./cmd/user-prompt-checker

# Run all unit and golden tests
test:
    go test ./...

# Format source code
fmt:
    go fmt ./...

# Run static analysis
vet:
    go vet ./...

# Run code quality checks (format check and vet)
lint: fmt vet

# Check a single user prompt (usage: just check "prompt" [history.json])
check prompt history="": build ollama::status
    ./user-prompt-checker check {{ quote(prompt) }} {{ if history != "" { "--history " + quote(history) } else { "" } }}

# Run prompt evaluation benchmark across cases.yaml
evaluate cases="cases.yaml" output="results.json": build ollama::status
    ./user-prompt-checker evaluate --cases {{ quote(cases) }} --output {{ quote(output) }}

# Register hook into an agent's configuration (agy or codex)
install agent="agy": build
    ./user-prompt-checker install {{ agent }}

# Remove hook from an agent's configuration (agy or codex)
uninstall agent="agy": build
    ./user-prompt-checker uninstall {{ agent }}

# Analyze prompt score improvements and quality history
gain format="text": build
    ./user-prompt-checker gain --format {{ format }}
