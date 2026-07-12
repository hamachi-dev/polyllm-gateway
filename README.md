# polyllm-gateway

> **Unofficial.** Not affiliated with Anthropic, OpenAI, or OpenCode.

Local LLM API proxy that translates OpenAI, Anthropic, and OpenAI Responses API requests to upstream LLM providers via OpenCode Go. Compatible with Claude Code 2.1+ and Codex CLI 0.144+.

## Architecture

```
Client (OpenAI API)    Client (Anthropic API)    Client (Responses API)
       │                       │                        │
       ▼                       ▼                        ▼
  OpenAI Handler         Anthropic Handler        Responses Handler
  /v1/chat/completions   /v1/messages             /v1/responses
       │                       │                        │
       └─────────────────────── Model Resolver ───────────────┘
                                     │
                                     ▼
                                Provider.Chat()
                                     │
                                     ▼
                              Upstream LLM API
```

## Setup

### 1. Build

```bash
cd ~/LLM/proxy
~/.local/go/bin/go build -o /tmp/proxy ./cmd/proxy/
```

### 2. Auto-start with systemd (recommended)

```bash
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/llm-proxy.service << 'EOF'
[Unit]
Description=LLM API Proxy
After=network.target

[Service]
Type=simple
ExecStart=/tmp/proxy /home/taizo/LLM/proxy/config.yaml
Restart=always
RestartSec=3
Environment=OPENCODE_API_KEY=your-key-here

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now llm-proxy
```

The proxy uses ~8MB RAM and 0% CPU idle. It auto-starts on login and auto-restarts on crash.

**Useful commands:**
```bash
systemctl --user status llm-proxy    # check status
systemctl --user restart llm-proxy   # restart after config change
journalctl --user -u llm-proxy -f    # tail logs
```

### 3. Run directly (alternative)

```bash
export OPENCODE_API_KEY="your-key"
/tmp/proxy ~/LLM/proxy/config.yaml
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
      deepseek-v4-flash:
        api: openai
      deepseek-v4-pro:
        api: openai
      mimo-v2.5:
        api: openai
      qwen3.7-plus:
        api: openai

routes:
  # Claude Code daily → MiMo V2.5 (fast)
  claude-sonnet-4-6:
    provider: opencode
    model: mimo-v2.5
  claude-haiku-4-5:
    provider: opencode
    model: mimo-v2.5

  # Claude Code high-quality → Qwen3.7 Plus
  claude-opus-4-8:
    provider: opencode
    model: qwen3.7-plus
  claude-fable-5:
    provider: opencode
    model: qwen3.7-plus

  # Codex CLI lightweight → DeepSeek V4 Flash
  gpt-5.4-mini:
    provider: opencode
    model: deepseek-v4-flash
  gpt-5.4-nano:
    provider: opencode
    model: deepseek-v4-flash

  # Codex CLI high-quality → DeepSeek V4 Pro
  gpt-5.5:
    provider: opencode
    model: deepseek-v4-pro
  gpt-5-codex:
    provider: opencode
    model: deepseek-v4-pro
```

### How `api` is resolved

1. If a route has `api` set, use that value
2. Otherwise, look up `providers.<name>.models.<model>.api`
3. Otherwise, default to `openai`

### Route fields

| Field | Description |
|-------|-------------|
| `provider` | Provider name from `providers` section |
| `model` | Actual model name sent to upstream |
| `api` | Upstream API format: `openai` or `anthropic` |

- `openai`: `Authorization: Bearer`, `/v1/chat/completions`
- `anthropic`: `x-api-key` + `anthropic-version: 2023-06-01`, `/v1/messages`

## Usage

### Claude Code

Set environment variables in `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8001",
    "ANTHROPIC_AUTH_TOKEN": "proxy",
    "ANTHROPIC_MODEL": "claude-sonnet-4-6",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-4-6",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-4-8",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-haiku-4-5"
  }
}
```

Then just run:
```bash
claude
```

### Codex CLI

Create `~/.codex/proxy.config.toml`:

```toml
model = "gpt-5.5"
model_provider = "proxy"

[model_providers.proxy]
name = "Proxy"
base_url = "http://localhost:8000/v1/"
```

Then launch:
```bash
codex --profile proxy
```

### Curl

OpenAI Chat Completions (port 8000):
```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4-mini","messages":[{"role":"user","content":"hello"}]}'
```

Anthropic Messages (port 8001):
```bash
curl http://localhost:8001/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"max_tokens":100}'
```

OpenAI Responses API (port 8000):
```bash
curl http://localhost:8000/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4-mini","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}'
```

## Model Routing

| Client Model | Upstream | Best For |
|-------------|----------|----------|
| `claude-sonnet-4-6` | MiMo V2.5 | Claude Code daily |
| `claude-opus-4-8` | Qwen3.7 Plus | Claude Code quality |
| `gpt-5.4-mini` | DeepSeek V4 Flash | Codex daily |
| `gpt-5.5` | DeepSeek V4 Pro | Codex complex |

## Format Conversion

### Anthropic → OpenAI

| Anthropic | OpenAI |
|-----------|--------|
| `system` (string or array) | `messages[0]` with `role: "system"` |
| `content` (string or array) | `content` (string, extracted from text blocks) |
| `tools[{name, input_schema}]` | `tools[{type:"function", function:{name, parameters}}]` |
| `tool_choice` | `tool_choice` |
| `developer` role | `system` role |

### OpenAI → Anthropic

| OpenAI | Anthropic |
|--------|-----------|
| `content` (string) | `content[{type:"text", text}]` |
| `reasoning_content` | Fallback when `content` is empty |
| `tool_calls[{id, function}]` | `content[{type:"tool_use", id, name, input}]` |
| `finish_reason: "stop"` | `stop_reason: "end_turn"` |
| `finish_reason: "tool_calls"` | `stop_reason: "tool_use"` |

### Streaming SSE

- **OpenAI upstream**: Raw SSE passthrough
- **Anthropic upstream**: Anthropic SSE → OpenAI SSE conversion
- **Responses API**: Chat Completions SSE → Responses SSE (`event:` + `output_index` + `item_id`)

## Logging

JSON structured logs to stdout. API keys and message content are never logged.
