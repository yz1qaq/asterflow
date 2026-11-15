package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yz1qaq/asterflow/internal/api"
	"github.com/yz1qaq/asterflow/internal/config"
	"github.com/yz1qaq/asterflow/internal/infrastructure/postgres"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
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

	store := iam.NewPostgresStore(pool)
	iamService := iam.NewService(store, []byte(cfg.JWTSecretKey))

	if cfg.AutoseedAdmin {
		seedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := iamService.EnsureSeedAdmin(
			seedCtx,
			cfg.AutoseedAdminEmail,
			cfg.AutoseedAdminPassword,
		)
		cancel()

		if err != nil {
			return fmt.Errorf("initialize admin: %w", err)
		}
	}

	handler := api.NewRouter(pool, iamService)
	log.Printf("starting HTTP server on %s", cfg.HTTPAddr)

	return run(ctx, handler, cfg.HTTPAddr)
}
