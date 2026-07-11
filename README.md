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
export OPENCODE_API_KEY="your-opencode-go-key"
make run
```

## Configuration

```yaml
listen:
  openai: ":8000"
  anthropic: ":8001"

providers:
  opencode:
    endpoint: https://opencode.ai/zen/go/v1
    api_key: ${OPENCODE_API_KEY}
    models:
      qwen3.7-plus:
        api: anthropic
      deepseek-v4-flash:
        api: openai

routes:
  gpt-5:
    provider: opencode
    model: qwen3.7-plus

  gpt-5-mini:
    provider: opencode
    model: deepseek-v4-flash
```

### How `api` is resolved

1. If a route has `api` set, use that value
2. Otherwise, look up `providers.<name>.models.<model>.api`
3. Otherwise, default to `openai`

This means routes don't need to specify `api` — it's defined once per model
in the provider config.

### Route fields

| Field | Description |
|-------|-------------|
| `provider` | Provider name from `providers` section |
| `model` | Actual model name sent to upstream |

### Provider model fields

| Field | Description |
|-------|-------------|
| `api` | Upstream API format: `openai` or `anthropic` |

- `openai`: `Authorization: Bearer`, `/v1/chat/completions`
- `anthropic`: `x-api-key` + `anthropic-version: 2023-06-01`, `/v1/messages`

## Usage Examples

OpenAI format (port 8000):
```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5-mini","messages":[{"role":"user","content":"hello"}]}'
```

Anthropic format (port 8001):
```bash
curl http://localhost:8001/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"max_tokens":100}'
```

Streaming:
```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5-mini","messages":[{"role":"user","content":"hello"}],"stream":true}'
```

## OpenCode Go API Notes

OpenCode Go uses different API formats per model:

| API Format | Auth Header | Endpoint |
|------------|-------------|----------|
| OpenAI | `Authorization: Bearer` | `/v1/chat/completions` |
| Anthropic | `x-api-key` | `/v1/messages` |

Models using Anthropic format: `qwen3.7-plus`, `qwen3.7-max`, `minimax-m2.7`
Models using OpenAI format: `deepseek-v4-flash`, `kimi-k2.6`, `glm-5.1`

## Streaming

- **OpenAI upstream**: Raw SSE passthrough
- **Anthropic upstream**: Anthropic SSE → OpenAI SSE conversion → Handler

## Logging

JSON structured logs to stdout. API keys and message content are never logged.
