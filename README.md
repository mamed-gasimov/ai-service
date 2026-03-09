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
│   │   └── rabbitmq/rabbitmq.go                 # RabbitMQ client (auto-reconnect, DLX setup)
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
  ACK the original message (only after successful publish)
        ↓
  [file-service consumes and updates PostgreSQL]
```

### Processing guarantees

- Messages are ACKed **only after** the reply is successfully published to `file.analysis.result`. If any step fails (download, OpenAI, marshal, publish), the message is NACKed to the dead-letter queue.
- The worker loop reconnects automatically if the RabbitMQ channel closes, with a 5-second backoff.

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

## Connection recovery

The RabbitMQ client automatically reconnects with exponential backoff (1s → 30s) when the connection drops. The connection and channel are protected by a `sync.RWMutex` so publishers and consumers are thread-safe during reconnection. After reconnecting, all exchanges and queues are re-declared.

The worker's outer `Run()` loop also handles channel-level closures: when the delivery channel closes, it re-calls `Consume()` to re-subscribe.

## Dead Letter Exchange (DLX)

Both services declare identical queue topology:

```
file.analyze  ──(nack)──▶  dlx exchange  ──▶  file.analyze.dlq
file.analysis.result  ──(nack)──▶  dlx exchange  ──▶  file.analysis.result.dlq
```

- Exchange: `dlx` (type: `direct`, durable)
- DLQ routing keys match DLQ names (e.g., `file.analyze.dlq`)
- Main queues set `x-dead-letter-exchange` and `x-dead-letter-routing-key` arguments

Messages land in DLQs when:
- Unmarshal fails (malformed JSON)
- MinIO download or OpenAI API call fails
- Reply publish to `file.analysis.result` fails

Use the RabbitMQ Management UI at `http://localhost:15672` to inspect accumulated messages.

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

## OpenAI integration

- **Model**: `gpt-4o-mini`
- **Task**: Translation + summarization — any language → English, 2–3 sentences
- **Input limit**: 100 000 bytes (content is truncated before sending)
- **System prompt**: instructs the model to respond only in English with a concise summary and no preamble

## Dependencies

| Package | Purpose |
|---|---|
| [echo/v4](https://github.com/labstack/echo) | Health check HTTP server |
| [amqp091-go](https://github.com/rabbitmq/amqp091-go) | RabbitMQ AMQP client |
| [minio-go/v7](https://github.com/minio/minio-go) | S3-compatible object storage client |
| [openai-go](https://github.com/openai/openai-go) | OpenAI API client |
| [caarlos0/env](https://github.com/caarlos0/env) | Struct-based env var parsing |
| [godotenv](https://github.com/joho/godotenv) | `.env` file loader |
