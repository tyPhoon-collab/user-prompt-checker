# Default: show ollama recipes
default:
    @just --list ollama

# Check Ollama service health and model availability
status model="d1:2b":
    @curl -sf http://localhost:11434/api/tags > /dev/null || (echo "Error: Ollama is not running on :11434" && exit 1)
    @curl -sf http://localhost:11434/api/tags | grep -q {{ quote(model) }} && echo "Ollama: OK (model '{{ model }}' available)" || (echo "Warning: model '{{ model }}' not found. Run 'just ollama pull'" && exit 1)

# Pull required model (defaults to d1:2b)
pull model="d1:2b":
    ollama pull {{ quote(model) }}
