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

## Usage

1. Create `config.yaml`:
```yaml
listen:
  openai: ":8000"
  anthropic: ":8001"

providers:
  opencode:
    endpoint: https://api.opencode.ai/v1
    api_key: ${OPENCODE_API_KEY}

routes:
  gpt-5:
    provider: opencode
    model: qwen3.7-plus
    api: openai

  claude-sonnet-4:
    provider: opencode
    model: deepseek-v4-flash
    api: anthropic
```

2. Run:
```bash
export OPENCODE_API_KEY="your-key"
make run
```

3. Send requests:

OpenAI format:
```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5","messages":[{"role":"user","content":"hello"}]}'
```

Anthropic format:
```bash
curl http://localhost:8001/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"max_tokens":100}'
```

## Configuration

### Listen
```yaml
listen:
  openai: ":8000"      # OpenAI API port
  anthropic: ":8001"   # Anthropic API port
```

### Providers
```yaml
providers:
  provider_name:
    endpoint: https://api.example.com/v1
    api_key: ${ENV_VAR}  # Supports env var expansion
```

### Routes
```yaml
routes:
  client-model-name:   # Model name sent by client
    provider: name     # Provider name from providers section
    model: actual-name # Actual model name sent to upstream
    api: openai        # Upstream API format: openai or anthropic
```

## Model Resolution

Every incoming request's model name is resolved through the routes table. If a model
is not configured, the proxy returns a 404 error.

## Streaming

Both OpenAI and Anthropic streaming are supported transparently.

## Logging

Structured JSON logs to stdout. Sensitive data (API keys, prompts) are never logged.
