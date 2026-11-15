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
	"github.com/yz1qaq/asterflow/internal/infrastructure/postgres"
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

	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer pool.Close()

	handler := api.NewRouter(pool)
	log.Printf("starting HTTP server on %s", cfg.HTTPAddr)

	return run(ctx, handler, cfg.HTTPAddr)
}
