# LLM API Proxy 設計書

## 1. アーキテクチャ

```
┌──────────────┐    ┌──────────────┐
│ OpenAI Client│    │Anthropic Cli.│
│ :8000        │    │ :8001        │
└──────┬───────┘    └──────┬───────┘
       │ POST              │ POST
       │ /v1/chat/         │ /v1/messages
       │ completions       │
       ▼                   ▼
┌──────────────────────────────────┐
│         API Handler              │
│  (Decode → Internal Request)     │
│  (Encode → Client Format)        │
└──────────────┬───────────────────┘
               │ model name
               ▼
┌──────────────────────────────────┐
│        Model Resolver            │
│  client_model → {provider,       │
│   upstream_model, api_schema}    │
└──────────────┬───────────────────┘
               │ provider name
               ▼
┌──────────────────────────────────┐
│          Provider                │
│  Chat(ctx, *ChatRequest)         │
│  → *ChatResponse                 │
│                                  │
│  ┌─ api=openai ──────────────┐   │
│  │ POST /v1/chat/completions │   │
│  │ Authorization: Bearer     │   │
│  └───────────────────────────┘   │
│  ┌─ api=anthropic ───────────┐   │
│  │ POST /v1/messages         │   │
│  │ x-api-key                 │   │
│  │ anthropic-version         │   │
│  └───────────────────────────┘   │
└──────┬───────────────────────────┘
       │
       ▼
┌──────────────────────────────────┐
│        Upstream LLM API          │
│  (OpenCode Go / OpenAI / etc.)   │
└──────────────────────────────────┘
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
    Model       string      // 上流に送るモデル名 (マッピング後)
    Messages    []Message
    Stream      bool
    Temperature *float64
    MaxTokens   *int
    Stop        []string
    API         string      // "openai" | "anthropic" - 上流のAPI形式
}

type ChatResponse struct {
    Model        string
    Message      Message
    FinishReason string
    Usage        *Usage
    StreamBody   io.ReadCloser  // Streaming時、OpenAI形式SSE
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
- ストリーミング: OpenAI SSE → Anthropic SSE変換 (OpenAItoAnthropic)
- エラー: Anthropic形式エラーレスポンス

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
    → json.Decode → ChatRequest{Model: "gpt-5-mini", Messages: [...], API: "openai"}
    → ModelResolver.Resolve("gpt-5-mini") → Route{Provider: "opencode", Model: "deepseek-v4-flash", API: "openai"}
    → OpenCodeProvider.Chat(ctx, ChatRequest{Model: "deepseek-v4-flash", API: "openai"})
      → chatOpenAI()
        → POST {endpoint}/chat/completions (Authorization: Bearer)
        → parseOpenAIResponse() → ChatResponse{Message: "Hello"}
    ← ChatResponse
  → json.Encode → OpenAI Chat Completions Response
  ← 200 OK
```

### 4.2 Non-Streaming (Anthropic Client → Anthropic Upstream)

```
Client POST /v1/messages
  → AnthropicHandler.ServeHTTP
    → json.Decode → ChatRequest{Model: "claude-sonnet-4", Messages: [...], API: "anthropic"}
    → ModelResolver.Resolve("claude-sonnet-4") → Route{Provider: "opencode", Model: "deepseek-v4-flash", API: "openai"}
    → OpenCodeProvider.Chat(ctx, ChatRequest{Model: "deepseek-v4-flash", API: "openai"})
      → chatOpenAI()
    ← ChatResponse
  → json.Encode → Anthropic Messages Response
  ← 200 OK
```

### 4.3 Streaming (OpenAI Client → Anthropic Upstream)

```
Client POST /v1/chat/completions {stream: true, model: "gpt-5"}
  → OpenAIHandler.ServeHTTP
    → ChatRequest{Model: "qwen3.7-plus", API: "anthropic", Stream: true}
    → OpenCodeProvider.Chat()
      → chatAnthropic()
        → POST {endpoint}/messages (x-api-key, anthropic-version, stream: true)
        → 上流からAnthropic SSE到着
        → io.ReadAll で全データ取得
        → AnthropicToOpenAI.Convert() → OpenAI形式SSE
        → StreamBody = OpenAI形式SSEのReader
    ← ChatResponse{StreamBody: OpenAI形式SSE}
  → CopyConverter.Convert(StreamBody, http.ResponseWriter)
  → ClientにOpenAI SSEをそのまま転送
```

### 4.4 Streaming (Anthropic Client → OpenAI Upstream)

```
Client POST /v1/messages {stream: true, model: "claude-sonnet-4"}
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

## 5. 設定ファイル構造

```yaml
listen:
  openai: ":8000"          # OpenAI API受付ポート
  anthropic: ":8001"       # Anthropic API受付ポート

providers:
  <provider_name>:         # Provider識別子 (routesから参照)
    endpoint: <url>        # 上流APIのベースURL
    api_key: ${ENV_VAR}    # APIキー (環境変数展開対応)
    models:                # モデル別設定 (省略可)
      <model_name>:
        api: <openai|anthropic>  # 上流のAPI形式

routes:
  <client_model_name>:     # クライアントが送信するモデル名
    provider: <name>       # providersのキー
    model: <name>          # 上流に送信するモデル名
    api: <openai|anthropic> # 省略可 → provider.models から解決
```

## 6. インターフェース設計

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

## 7. エラーハンドリング

Providerエラー発生時、呼び出し元のAPI HandlerがClientの期待する形式でエラーレスポンスを返す。

| Handler | エラー形式 |
|---------|-----------|
| OpenAI | `{"error": {"message": "...", "type": "api_error"}}` |
| Anthropic | `{"type": "error", "error": {"type": "api_error", "message": "..."}}` |

## 8. 将来追加予定との対応

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
