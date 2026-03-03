package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mamed-gasimov/ai-service/internal/config"
	"github.com/mamed-gasimov/ai-service/internal/messaging/rabbitmq"
	"github.com/mamed-gasimov/ai-service/internal/modules/translation/openai"
	"github.com/mamed-gasimov/ai-service/internal/server"
	miniostorage "github.com/mamed-gasimov/ai-service/internal/storage/minio"
	"github.com/mamed-gasimov/ai-service/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// --- Object Storage (MinIO) ---------------------------------------------
	store, err := miniostorage.New(
		cfg.Minio.Endpoint,
		cfg.Minio.AccessKey,
		cfg.Minio.SecretKey,
		cfg.Minio.Bucket,
		cfg.Minio.UseSSL,
	)
	if err != nil {
		return fmt.Errorf("init minio: %w", err)
	}

	if err := store.EnsureBucket(context.Background(), cfg.Minio.Bucket); err != nil {
		return fmt.Errorf("ensure bucket: %w", err)
	}
	log.Printf("MinIO bucket %q is ready\n", cfg.Minio.Bucket)

	// --- Messaging (RabbitMQ) -----------------------------------------------
	broker, err := rabbitmq.NewClient(cfg.RabbitMQ.URL)
	if err != nil {
		return fmt.Errorf("connect to rabbitmq: %w", err)
	}
	defer broker.Close()

	// --- AI (OpenAI) --------------------------------------------------------
	translator := openai.NewTranslator(cfg.OpenAI.APIKey, cfg.OpenAI.BaseURL)

	// --- Worker -------------------------------------------------------------
	w := worker.New(broker, store, translator)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	go w.Run(workerCtx)

	// --- Health server ------------------------------------------------------
	e := server.New()
	go func() {
		addr := ":" + cfg.ServerPort
		log.Printf("health server on %s\n", addr)
		if err := e.Start(addr); err != nil {
			log.Printf("health server stopped: %v\n", err)
		}
	}()

	// --- Graceful shutdown --------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down …")
	workerCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	return e.Shutdown(shutdownCtx)
}
