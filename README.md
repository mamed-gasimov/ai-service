# ai-service

Go 1.25 worker service that consumes file analysis requests from RabbitMQ, downloads files from MinIO, generates English translation summaries via OpenAI (GPT-4o Mini), and publishes results back through RabbitMQ.

This service is the async AI processing backend for **file-service**. It has no database and no REST write endpoints — it is purely message-driven.

## Project structure

```
ai-service/
├── cmd/worker/main.go                           # entry point – wires everything together
├── internal/
│   ├── config/config.go                         # .env → Config struct (caarlos0/env)
│   ├── messaging/
│   │   ├── messaging.go                         # Publisher/Consumer interfaces
│   │   └── rabbitmq/rabbitmq.go                 # RabbitMQ client (publish + consume)
│   ├── modules/
│   │   └── translation/
│   │       ├── provider.go                      # Translator interface
│   │       ├── model.go                         # AnalyzeRequest / AnalysisReply message types
│   │       └── openai/openai.go                 # OpenAI implementation (GPT-4o Mini)
│   ├── server/server.go                         # Echo HTTP server (health check only)
│   ├── storage/
│   │   ├── storage.go                           # Storage interface
│   │   └── minio/minio.go                       # MinIO implementation
│   └── worker/worker.go                         # core processing loop
├── .env.example                                 # environment variable template
└── go.mod
```

## Quick start

```bash
# 1. Ensure the shared infrastructure is running (from file-service)
cd ../file-service && docker compose up -d

# 2. Copy env (adjust if needed)
cp .env.example .env

# 3. Run the worker
go run ./cmd/worker
```

The worker starts consuming from the `file.analyze` queue immediately. A health check endpoint is available at `http://localhost:8081/health`.

## How it works

```
[file.analyze queue]
        ↓
  worker.go – Run()
        ↓
  Download file from MinIO (max 100 KB)
        ↓
  Send content to OpenAI (GPT-4o Mini)
  → 2–3 sentence English summary
        ↓
  Publish AnalysisReply → [file.analysis.result queue]
        ↓
  [file-service consumes and updates PostgreSQL]
```

The worker loop reconnects automatically if the RabbitMQ channel closes, with a 5-second backoff. Each message is acknowledged only after a successful reply is published.

## Configuration

All settings are loaded from environment variables (or an `.env` file via godotenv).

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8081` | Health check HTTP port |
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO API endpoint |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO access key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO secret key |
| `MINIO_BUCKET` | `files` | MinIO bucket name |
| `MINIO_USE_SSL` | `false` | Use SSL for MinIO |
| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection URL |
| `OPENAI_API_KEY` | — | OpenAI API key (required) |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible API base URL |

## API

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Returns `{"status":"ok"}` — used by orchestrators |

## RabbitMQ message contracts

Both queues are declared `durable` with `persistent` delivery mode.

### Consumed: `file.analyze`

```json
{
  "file_id": 1,
  "object_key": "2026/02/16/uuid_myfile.pdf",
  "content_type": "application/pdf",
  "correlation_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Published: `file.analysis.result`

```json
{
  "file_id": 1,
  "translation_summary": "This document describes...",
  "correlation_id": "550e8400-e29b-41d4-a716-446655440000",
  "error": ""
}
```

If processing fails, `translation_summary` is empty and `error` contains the failure message. file-service logs the error and skips the database update.

## OpenAI integration

- **Model**: `gpt-4o-mini`
- **Task**: Translation + summarization — any language → English, 2–3 sentences
- **Input limit**: 100 000 bytes (content is truncated before sending)
- **System prompt**: instructs the model to respond only in English with a concise summary and no preamble

## Dead Letter Exchange (DLX) / Dead Letter Queue (DLQ)

### Current state

Neither this service nor file-service currently implement DLX/DLQ. Failed messages are acknowledged and dropped:

- Unmarshal error → message lost
- MinIO download failure → error published to reply queue, original message ACKed
- OpenAI API failure → same as above

### Why DLX/DLQ matters

Without dead-letter routing, transient failures (network blip, OpenAI rate limit, MinIO timeout) permanently lose work. A DLX/DLQ setup captures failed messages for inspection and retry.

### Implementation guide

The approach requires changes in both services since they share the same queue declarations. Whichever service declares a queue first must include the DLX arguments.

#### Step 1 — Declare the dead letter exchange and queue

In `internal/messaging/rabbitmq/rabbitmq.go` of **both** services, replace the current plain queue declarations with:

```go
// 1. Declare the DLX (a durable direct exchange)
if err := ch.ExchangeDeclare(
    "dlx",    // name
    "direct", // type
    true,     // durable
    false,    // auto-delete
    false,    // internal
    false,    // no-wait
    nil,
); err != nil {
    return nil, fmt.Errorf("declare dlx: %w", err)
}

// 2. Declare dead letter queues (one per main queue)
for _, dlq := range []string{"file.analyze.dlq", "file.analysis.result.dlq"} {
    if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
        return nil, fmt.Errorf("declare dlq %s: %w", dlq, err)
    }
    if err := ch.QueueBind(dlq, dlq, "dlx", false, nil); err != nil {
        return nil, fmt.Errorf("bind dlq %s: %w", dlq, err)
    }
}

// 3. Declare main queues with DLX and TTL arguments
dlxArgs := amqp.Table{
    "x-dead-letter-exchange":     "dlx",
    "x-dead-letter-routing-key":  "",   // set per queue below
    "x-message-ttl":              86_400_000, // 24 h in ms
}
for _, q := range []string{"file.analyze", "file.analysis.result"} {
    args := amqp.Table{
        "x-dead-letter-exchange":    "dlx",
        "x-dead-letter-routing-key": q + ".dlq",
        "x-message-ttl":             int32(86_400_000),
    }
    if _, err := ch.QueueDeclare(q, true, false, false, false, args); err != nil {
        return nil, fmt.Errorf("declare queue %s: %w", err)
    }
}
```

> **Note**: Queue arguments are immutable after creation. If the queues already exist without DLX args, you must delete and re-declare them (this requires a brief maintenance window or a new queue name strategy).

#### Step 2 — NACK instead of ACK on processing failures

In `internal/worker/worker.go`, change the error path from `d.Ack` to `d.Nack` so the broker routes the message to the DLQ:

```go
// Before (message lost)
if err := w.process(ctx, &req); err != nil {
    // publish error reply, then...
    d.Ack(false)
}

// After (message routed to DLQ)
if err := w.process(ctx, &req); err != nil {
    // publish error reply for the caller, and...
    d.Nack(false, false) // requeue=false → triggers DLX routing
    continue
}
d.Ack(false)
```

Apply the same pattern in file-service's `result_consumer.go`.

#### Step 3 — Retry worker (optional)

Add a separate goroutine (or a new service) that consumes from `file.analyze.dlq`, waits for a backoff period, and republishes messages to `file.analyze`:

```go
func retryDLQ(ctx context.Context, broker messaging.Consumer, delay time.Duration) {
    msgs, _ := broker.Consume(ctx, "file.analyze.dlq")
    for d := range msgs {
        select {
        case <-ctx.Done():
            return
        case <-time.After(delay):
            broker.Publish(ctx, "", "file.analyze", d.Body)
            d.Ack(false)
        }
    }
}
```

#### Step 4 — Monitor the DLQs

Use the RabbitMQ Management UI at `http://localhost:15672` (guest/guest) to watch `file.analyze.dlq` and `file.analysis.result.dlq` for accumulated messages. Alert when queue depth exceeds a threshold.

### Summary of queue topology after DLX

```
file.analyze  ──(nack/ttl)──▶  dlx exchange  ──▶  file.analyze.dlq
file.analysis.result  ────────▶  dlx exchange  ──▶  file.analysis.result.dlq
```

## Dependencies

| Package | Purpose |
|---|---|
| [echo/v4](https://github.com/labstack/echo) | Health check HTTP server |
| [amqp091-go](https://github.com/rabbitmq/amqp091-go) | RabbitMQ AMQP client |
| [minio-go/v7](https://github.com/minio/minio-go) | S3-compatible object storage client |
| [openai-go](https://github.com/openai/openai-go) | OpenAI API client |
| [caarlos0/env](https://github.com/caarlos0/env) | Struct-based env var parsing |
| [godotenv](https://github.com/joho/godotenv) | `.env` file loader |
