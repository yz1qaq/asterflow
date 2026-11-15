package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yz1qaq/asterflow/internal/api"
	"github.com/yz1qaq/asterflow/internal/config"
)

func main() {
	if err := start(); err != nil {
		log.Printf("asterflow server: %v", err)
		os.Exit(1)
	}
}

func start() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	handler := api.NewRouter()
	log.Printf("starting HTTP server on %s", cfg.HTTPAddr)

	return run(ctx, handler, cfg.HTTPAddr)
}
