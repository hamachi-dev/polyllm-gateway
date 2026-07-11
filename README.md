# LLM API Proxy

Local LLM API proxy that translates OpenAI and Anthropic API requests to upstream LLM providers.

## Architecture

```
Client (OpenAI API)    Client (Anthropic API)
       │                       │
       ▼                       ▼
 OpenAI Handler         Anthropic Handler
       │                       │
       └─────── Model Resolver ───────┘
                      │
                      ▼
                 Provider.Chat()
                      │
                      ▼
              Upstream LLM API
```

## Quick Start

```bash
# Set your API key
export OPENCODE_API_KEY="your-opencode-go-key"

# Start the proxy
make run
```

Then send requests in either API format.

## Configuration

```yaml
listen:
  openai: ":8000"       # OpenAI API endpoint
  anthropic: ":8001"    # Anthropic API endpoint

providers:
  opencode:
    endpoint: https://opencode.ai/zen/go/v1
    api_key: ${OPENCODE_API_KEY}

routes:
  # OpenAI-compatible client → OpenAI upstream
  gpt-5-mini:
    provider: opencode
    model: deepseek-v4-flash
    api: openai

  # Anthropic-compatible client → OpenAI upstream
  claude-sonnet-4:
    provider: opencode
    model: deepseek-v4-flash
    api: openai

  # Any client → Anthropic upstream (e.g. qwen3.7-plus uses Anthropic Messages API)
  gpt-5:
    provider: opencode
    model: qwen3.7-plus
    api: anthropic
```

### Route fields

| Field | Description |
|-------|-------------|
| `provider` | Provider name from `providers` section |
| `model` | Actual model name sent to upstream |
| `api` | Upstream API format: `openai` or `anthropic` |

The `api` field determines how the provider formats the request to the upstream.
- `openai`: Uses `Authorization: Bearer` header, sends to `/chat/completions`
- `anthropic`: Uses `x-api-key` header with `anthropic-version: 2023-06-01`, sends to `/messages`

## Usage Examples

### OpenAI format (port 8000)

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5-mini","messages":[{"role":"user","content":"hello"}]}'
```

### Anthropic format (port 8001)

```bash
curl http://localhost:8001/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"max_tokens":100}'
```

### Streaming (both formats)

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5-mini","messages":[{"role":"user","content":"hello"}],"stream":true}'
```

## OpenCode Go API Notes

OpenCode Go uses **two different API formats** depending on the model:

| API Format | Auth Header | Endpoint | Models |
|------------|------------|----------|--------|
| OpenAI | `Authorization: Bearer` | `/v1/chat/completions` | deepseek-v4-flash, kimi-k2.6, glm-5.1, etc. |
| Anthropic | `x-api-key` | `/v1/messages` | qwen3.7-plus, qwen3.7-max, minimax-m2.7 |

Set `api: anthropic` for models that require Anthropic Messages API.

## Model Resolution

Every incoming model name is resolved through the routes table. If a model is not
configured, the proxy returns a 404 error. No model names are hardcoded in the
application code.

## Streaming

- **OpenAI → OpenAI**: Raw SSE passthrough
- **Anthropic → OpenAI**: OpenAI SSE format passthrough
- **Anthropic → Anthropic**: OpenAI SSE → Anthropic SSE conversion

## Logging

Structured JSON logs to stdout. API keys and message content are never logged.
