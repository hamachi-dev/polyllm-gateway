# polyllm-gateway 設計書

## 1. アーキテクチャ

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│ OpenAI Client│    │Anthropic Cli.│    │ Codex CLI    │
│ :8000        │    │ :8001        │    │ :8000        │
└──────┬───────┘    └──────┬───────┘    └──────┬───────┘
       │ POST              │ POST              │ POST
       │ /v1/chat/         │ /v1/messages      │ /v1/responses
       │ completions       │                   │
       ▼                   ▼                   ▼
┌──────────────────────────────────────────────────────┐
│                  API Handler                         │
│  (Decode → Internal Request)                         │
│  (Encode → Client Format)                            │
└──────────────────┬───────────────────────────────────┘
                   │ model name
                   ▼
┌──────────────────────────────────────────────────────┐
│                 Model Resolver                        │
│  client_model → {provider,                           │
│   upstream_model, api_schema}                        │
└──────────────────┬───────────────────────────────────┘
                   │ provider name
                   ▼
┌──────────────────────────────────────────────────────┐
│                    Provider                           │
│  Chat(ctx, *ChatRequest)                             │
│  → *ChatResponse                                     │
│                                                      │
│  ┌─ api=openai ──────────────────────────────────┐   │
│  │ POST /v1/chat/completions                     │   │
│  │ Authorization: Bearer                         │   │
│  └───────────────────────────────────────────────┘   │
│  ┌─ api=anthropic ───────────────────────────────┐   │
│  │ POST /v1/messages                             │   │
│  │ x-api-key                                     │   │
│  │ anthropic-version                             │   │
│  └───────────────────────────────────────────────┘   │
└──────┬───────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────┐
│               Upstream LLM API                       │
│  (OpenCode Go / OpenAI / etc.)                       │
└──────────────────────────────────────────────────────┘
```

## 2. ディレクトリ構成

```
proxy/
├── cmd/
│   └── proxy/
│       └── main.go                  # エントリーポイント
├── internal/
│   ├── model/
│   │   └── chat.go                  # Internal Request/Response
│   ├── config/
│   │   ├── config.go                # YAML読込 + env展開 + model→api解決
│   │   └── config_test.go
│   ├── resolver/
│   │   ├── model.go                 # ModelResolver
│   │   └── model_test.go
│   ├── provider/
│   │   ├── provider.go              # Provider interface
│   │   ├── opencode.go              # OpenCode Go実装 (OpenAI/Anthropic両形式対応)
│   │   └── opencode_test.go
│   ├── api/
│   │   ├── openai.go                # POST /v1/chat/completions
│   │   ├── anthropic.go             # POST /v1/messages
│   │   ├── responses.go             # POST /v1/responses (Codex CLI互換)
│   │   ├── openai_test.go
│   │   ├── anthropic_test.go
│   │   └── helpers_test.go
│   ├── stream/
│   │   ├── stream.go                # Converter interface
│   │   ├── copy.go                  # Passthrough copy
│   │   ├── openai_to_anthropic.go   # OpenAI SSE → Anthropic SSE
│   │   ├── anthropic_to_openai.go   # Anthropic SSE → OpenAI SSE
│   │   └── *_test.go
│   └── logger/
│       ├── logger.go                # slog JSON logger
│       └── logger_test.go
├── config.yaml                      # 設定サンプル
├── DESIGN.md                        # 本設計書
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── README.md
├── go.mod
└── go.sum
```

## 3. パッケージ設計

### 3.1 model (内部ドメインモデル)

内部転送用の共通データ構造。API HandlerがClient形式からデコードした結果を保持し、Providerへ渡す。

```go
type ChatRequest struct {
    Model       string
    Messages    []Message
    Stream      bool
    Temperature *float64
    MaxTokens   *int
    Stop        []string
    API         string      // "openai" | "anthropic"
    Tools       []ToolDef
    ToolChoice  string
}

type ToolDef struct {
    Name        string
    Description string
    Parameters  json.RawMessage
}

type Message struct {
    Role       string
    Content    string
    ToolCalls  []ToolCall
    ToolCallID string
}

type ChatResponse struct {
    Model        string
    Message      Message
    FinishReason string
    Usage        *Usage
    StreamBody   io.ReadCloser
}
```

### 3.2 config (設定読込)

YAMLファイルを読み込み、環境変数を展開し、ルートのAPI形式を解決する。

解決順序:
1. `route.api` が明示指定 → それを使用
2. `providers.<name>.models.<model>.api` → それを使用
3. デフォルト値 `"openai"`

```go
type ModelConfig struct {
    API string `yaml:"api"`
}

type ProviderConfig struct {
    Endpoint string                 `yaml:"endpoint"`
    APIKey   string                 `yaml:"api_key"`
    Models   map[string]ModelConfig `yaml:"models,omitempty"`
}

type RouteConfig struct {
    Provider string `yaml:"provider"`
    Model    string `yaml:"model"`
    API      string `yaml:"api,omitempty"`  // 未指定時は解決される
}
```

### 3.3 resolver (モデル名解決)

クライアントからのモデル名を、Provider + 上流モデル名 + API形式に変換する。

```go
type Route struct {
    Provider string
    Model    string
    API      string
}

type ModelResolver struct {
    routes map[string]Route
}

func (r *ModelResolver) Resolve(model string) (Route, bool)
```

### 3.4 provider (上流通信)

Provider interfaceを実装し、上流LLM APIと通信する。

```go
type Provider interface {
    Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error)
}
```

OpenCodeProviderは `req.API` の値によって以下の2系統を切り替える:

| req.API | Endpoint | Auth Header | その他Header |
|---------|----------|-------------|--------------|
| `"openai"` | `/chat/completions` | `Authorization: Bearer` | |
| `"anthropic"` | `/messages` | `x-api-key` | `anthropic-version: 2023-06-01` |

Streaming時:
- `api=openai`: 上流のSSEをそのままStreamBodyにセット (CopyConverter)
- `api=anthropic`: 上流のAnthropic SSEを読み取り、OpenAI形式SSEに変換してStreamBodyにセット (AnthropicToOpenAI)

### 3.5 api (HTTP Handler)

ClientからのHTTPリクエストを受け取り、Internal Requestに変換してProviderへ渡す。 レスポンスはClientが期待する形式にエンコードする。

**OpenAI Handler** (`POST /v1/chat/completions`):
- リクエスト: OpenAI Chat Completions JSON
- ストリーミング: CopyConverter (そのまま転送)
- エラー: OpenAI形式エラーレスポンス

**Anthropic Handler** (`POST /v1/messages`):
- リクエスト: Anthropic Messages JSON
- `system` フィールド: `string` と `[{type:"text", text:"..."}]` の両形式対応
- `content` フィールド: `string` と `[{type:"text", text:"..."}]` の両形式対応 (Claude Code 2.1+ 互換)
- `tools` 変換: Anthropic `{name, input_schema}` → OpenAI `{type:"function", function:{name, parameters}}`
- `tool_choice` 変換: そのまま転送
- `developer` ロール: 自動的に `system` ロールに変換 (上流非対応のため)
- レスポンス変換: `tool_calls` → `tool_use` content block, `stop_reason` マッピング
- ストリーミング: OpenAI SSE → Anthropic SSE変換 (OpenAItoAnthropic)
- エラー: Anthropic形式エラーレスポンス

**Responses Handler** (`POST /v1/responses`):
- リクエスト: OpenAI Responses API JSON
- `instructions` → system メッセージに変換
- `input` 配列 → messages 配列に変換 (content block 対応)
- 非ストリーミング: Responses API JSON レスポンス
- ストリーミング: Chat Completions SSE → Responses SSE 変換
  - `event:` プレフィックス + `data:` ペイロード
  - `response.created` → `response.output_item.added` → `response.output_text.delta` → `response.output_item.done` → `response.completed`
  - MetaFARS/codex-relay 互換の最小イベントセット

### 3.6 stream (ストリーミング変換)

```go
type Converter interface {
    Convert(ctx context.Context, src io.Reader, dst io.Writer) error
}
```

| 実装 | 変換方向 | 用途 |
|------|---------|------|
| CopyConverter | そのまま | OpenAI Handler の streaming |
| OpenAItoAnthropic | OpenAI SSE → Anthropic SSE | Anthropic Handler の streaming |
| AnthropicToOpenAI | Anthropic SSE → OpenAI SSE | Provider 内部 (api=anthropic時) |
| Responses SSE変換 | Chat Completions SSE → Responses SSE | Responses Handler の streaming |

### 3.7 logger (ログ)

`slog` + JSON Handler。以下のフィールドを出力:
- `timestamp`
- `request_id`
- `provider`
- `route` (client model名)
- `api` (openai/anthropic)
- `latency`
- `status_code`

APIキーとメッセージ本文は絶対に出力しない。

## 4. データフロー詳細

### 4.1 Non-Streaming (OpenAI Client → OpenAI Upstream)

```
Client POST /v1/chat/completions
  → OpenAIHandler.ServeHTTP
    → json.Decode → ChatRequest{Model: "gpt-5.4-mini", Messages: [...], API: "openai"}
    → ModelResolver.Resolve("gpt-5.4-mini") → Route{Provider: "opencode", Model: "deepseek-v4-flash", API: "openai"}
    → OpenCodeProvider.Chat(ctx, ChatRequest{Model: "deepseek-v4-flash", API: "openai"})
      → chatOpenAI()
        → POST {endpoint}/chat/completions (Authorization: Bearer)
        → parseOpenAIResponse() → ChatResponse{Message: "Hi"}
    ← ChatResponse
  → json.Encode → OpenAI Chat Completions Response
  ← 200 OK
```

### 4.2 Non-Streaming (Anthropic Client → OpenAI Upstream)

```
Client POST /v1/messages {tools: [...], system: "...", messages: [...]}
  → AnthropicHandler.ServeHTTP
    → system: string|array → テキスト抽出
    → content: string|array → テキスト抽出
    → tools[{name, input_schema}] → ToolDef[{name, parameters}]
    → developer → system ロール変換
    → ChatRequest{Model: "deepseek-v4-flash", API: "openai", Tools: [...]}
    → ModelResolver.Resolve("claude-sonnet-4-6")
    → OpenCodeProvider.Chat()
      → Anthropic tools → OpenAI tools 変換
      → POST /chat/completions
    ← ChatResponse{Message: {Content: "Hi", ToolCalls: [...]}}
  → tool_calls → tool_use content block 変換
  → finish_reason "stop" → "end_turn", "tool_calls" → "tool_use"
  → json.Encode → Anthropic Messages Response
  ← 200 OK
```

### 4.3 Non-Streaming (Responses API Client → OpenAI Upstream)

```
Client POST /v1/responses
  → ResponsesHandler.ServeHTTP
    → instructions → system メッセージ
    → input[type:"message"] messages → messages 抽出
    → developer → system ロール変換
    → ChatRequest{Model: "deepseek-v4-flash", API: "openai"}
    → OpenCodeProvider.Chat(ctx, {...})
    ← ChatResponse
  → json.Encode → Responses API Response
    {id, object:"response", output:[{type:"message", content:[{type:"output_text", text:"Hi"}]}]}
  ← 200 OK
```

### 4.4 Streaming (Responses API Client → OpenAI Upstream)

```
Client POST /v1/responses {stream: true}
  → ResponsesHandler.ServeHTTP
    → ChatRequest{Stream: true}
    → OpenCodeProvider.Chat()
      → Chat Completions SSE 到着
        data: {"choices":[{"delta":{"role":"assistant",...}}],...}
        ...
        data: [DONE]
    ← ChatResponse{StreamBody: OpenAI形式SSE Reader}
  → handleStreaming()
    → Chat Completions SSE パース
    → Responses SSE イベントに変換して送信:
        event: response.created
        event: response.output_item.added
        event: response.output_text.delta
        event: response.output_item.done
        event: response.completed
  ← 200 OK (text/event-stream)
```

### 4.5 Streaming (Anthropic Client → OpenAI Upstream)

```
Client POST /v1/messages {stream: true, model: "claude-sonnet-4-6"}
  → AnthropicHandler.ServeHTTP
    → ChatRequest{Model: "deepseek-v4-flash", API: "openai", Stream: true}
    → OpenCodeProvider.Chat()
      → chatOpenAI()
        → POST {endpoint}/chat/completions (stream: true)
        → StreamBody = 上流のOpenAI SSE (そのまま)
    ← ChatResponse{StreamBody: OpenAI形式SSE}
  → OpenAItoAnthropic.Convert(StreamBody, ResponseWriter)
    → OpenAI SSE chunk → Anthropic SSE event に1:1変換
  → ClientにAnthropic SSEを転送
```

## 5. フォーマット変換

### Anthropic → OpenAI (リクエスト)

| Anthropic | OpenAI |
|-----------|--------|
| `system` (string or array) | `messages[0]` with `role: "system"` |
| `content[{type:"text", text}]` | `content` (string, extracted from text blocks) |
| `tools[{name, input_schema}]` | `tools[{type:"function", function:{name, parameters}}]` |
| `tool_choice` | `tool_choice` |
| `developer` role | `system` role |

### OpenAI → Anthropic (レスポンス)

| OpenAI | Anthropic |
|--------|-----------|
| `content` (string) | `content[{type:"text", text}]` |
| `reasoning_content` | `content` が空の場合のフォールバック |
| `tool_calls[{id, function:{name, arguments}}]` | `content[{type:"tool_use", id, name, input}]` |
| `finish_reason: "stop"` | `stop_reason: "end_turn"` |
| `finish_reason: "tool_calls"` | `stop_reason: "tool_use"` |
| `finish_reason: "length"` | `stop_reason: "max_tokens"` |

## 6. 設定ファイル構造

```yaml
listen:
  openai: ":8000"
  anthropic: ":8001"

providers:
  <provider_name>:
    endpoint: <url>
    api_key: ${ENV_VAR}
    models:
      <model_name>:
        api: <openai|anthropic>

routes:
  <client_model>:
    provider: <name>
    model: <name>
    api: <openai|anthropic>   # 省略可
```

### モデルルーティング戦略

| クライアント | 上流モデル | API形式 | 価格 (in/out) | 選定理��� |
|-------------|-----------|---------|---------------|---------|
| Claude Code (sonnet) | MiMo V2.5 | openai | 高速・安価・非推論 |
| Claude Code (opus) | Qwen3.7 Plus | openai | 高品質・tool互換 |
| Codex CLI (mini) | DeepSeek V4 Flash | openai | 推論付きコーディング |
| Codex CLI (pro) | DeepSeek V4 Pro | openai | 重量推論タスク |

- Claude Code は自身が推論・計画を管理するため、上流に推論モデルは不要（二重推論で遅延悪化）
- Codex CLI はコーディング特化のため、推論モデルの恩恵を受けやすい

## 7. インターフェース設計

```go
// Provider: 上流LLM APIとの通信を抽象化
type Provider interface {
    Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error)
}

// Converter: ストリーミング形式の変換を抽象化
type Converter interface {
    Convert(ctx context.Context, src io.Reader, dst io.Writer) error
}

// ModelResolver: モデル名の解決を抽象化
type ModelResolver struct { ... }  // 現在はstruct、必要に応じてinterface化
```

## 8. エラーハンドリング

Providerエラー発生時、呼び出し元のAPI HandlerがClientの期待する形式でエラーレスポンスを返す。

| Handler | エラー形式 |
|---------|-----------|
| OpenAI | `{"error": {"message": "...", "type": "api_error"}}` |
| Anthropic | `{"type": "error", "error": {"type": "api_error", "message": "..."}}` |
| Responses | 同上 (OpenAI形式) |

## 9. 将来追加予定との対応

| 機能 | 影響範囲 | 追加方法 |
|------|---------|---------|
| OpenRouter Provider | `internal/provider/` | Provider interface実装 + providers設定追加 |
| Ollama Provider | `internal/provider/` | 同上 |
| OpenAI Provider | `internal/provider/` | 同上 (通常のOpenAI API) |
| Anthropic Provider | `internal/provider/` | chatAnthropicと同様の実装 |
| Gemini Provider | `internal/provider/` | 同上 |
| リトライ | `internal/provider/` or ラッパーProvider | RetryProviderでラップ |
| タイムアウト | `internal/provider/` | context.WithTimeout |
| Rate Limit | 同上 | RateLimitProviderでラップ |
| キャッシュ | 同上 | CacheProviderでラップ |
| Prometheus | `internal/api/` or middleware | metricsミドルウェア追加 |
| モデル自動切替 | `internal/resolver/` | フェイルオーバー解決ロジック追加 |
| 負荷分散 | `internal/resolver/` | 複数Providerの重み付き選択 |
| Responses API ツール呼出対応 | `internal/api/responses.go` | `function_call`/`custom_tool_call` イベント追加 |
| Responses API リーズニング対応 | `internal/api/responses.go` | `reasoning_summary_text.delta` イベント追加 |
