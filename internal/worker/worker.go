package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/mamed-gasimov/ai-service/internal/messaging"
	"github.com/mamed-gasimov/ai-service/internal/modules/translation"
	"github.com/mamed-gasimov/ai-service/internal/storage"
)

const maxContentLen = 100_000

type broker interface {
	messaging.Consumer
	messaging.Publisher
}

type Worker struct {
	broker     broker
	store      storage.Storage
	translator translation.Translator
}

func New(b broker, store storage.Storage, translator translation.Translator) *Worker {
	return &Worker{
		broker:     b,
		store:      store,
		translator: translator,
	}
}

// Run starts the consume loop and blocks until ctx is cancelled.
// It reconnects automatically if the message channel closes.
func (w *Worker) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		msgs, err := w.broker.Consume(ctx, "file.analyze")
		if err != nil {
			log.Printf("worker: consume error: %v — retrying in 5s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		log.Println("worker: listening on file.analyze")
		for del := range msgs {
			w.handle(ctx, del)
		}
		log.Println("worker: message channel closed, reconnecting…")
	}
}

func (w *Worker) handle(ctx context.Context, del messaging.Delivery) {
	var req translation.AnalyzeRequest
	if err := json.Unmarshal(del.Body, &req); err != nil {
		log.Printf("worker: unmarshal request: %v — nacking to DLQ", err)
		del.Nack()
		return
	}

	log.Printf("worker: processing file %d object_key=%s", req.FileID, req.ObjectKey)

	summary, err := w.process(ctx, req)
	if err != nil {
		log.Printf("worker: process file %d: %v — nacking to DLQ", req.FileID, err)
		del.Nack()
		return
	}

	reply := translation.AnalysisReply{
		FileID:             req.FileID,
		CorrelationID:      req.CorrelationID,
		TranslationSummary: summary,
	}

	replyBody, err := json.Marshal(reply)
	if err != nil {
		log.Printf("worker: marshal reply for file %d: %v — nacking to DLQ", req.FileID, err)
		del.Nack()
		return
	}

	if err := w.broker.Publish(ctx, "", "file.analysis.result", replyBody); err != nil {
		log.Printf("worker: publish reply for file %d: %v — nacking to DLQ", req.FileID, err)
		del.Nack()
		return
	}

	del.Ack()
	log.Printf("worker: published translation result for file %d", req.FileID)
}

func (w *Worker) process(ctx context.Context, req translation.AnalyzeRequest) (string, error) {
	rc, err := w.store.Download(ctx, req.ObjectKey)
	if err != nil {
		return "", fmt.Errorf("download %q: %w", req.ObjectKey, err)
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read content: %w", err)
	}

	text := string(content)
	if len(text) > maxContentLen {
		text = text[:maxContentLen]
	}

	return w.translator.Translate(ctx, text)
}
