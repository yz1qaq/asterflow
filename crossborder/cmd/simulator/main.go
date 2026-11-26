package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	httpapi "github.com/yz1qaq/asterflow/crossborder/internal/api"
	"github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres"
	pgstore "github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres/sqlc"
	"github.com/yz1qaq/asterflow/crossborder/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	databaseURL := strings.TrimSpace(
		os.Getenv("CROSSBORDER_DATABASE_URL"),
	)
	if databaseURL == "" {
		return fmt.Errorf("CROSSBORDER_DATABASE_URL is required")
	}

	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open business database: %w", err)
	}
	defer pool.Close()

	q := pgstore.New(pool)
	store := service.NewPostgresStore(pool, q)
	svc := service.NewService(store)

	addr := strings.TrimSpace(os.Getenv("CROSSBORDER_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8091"
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(svc),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen business HTTP: %w", err)
	}
	defer listener.Close()

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(listener)
	}()

	log.Printf(
		"crossborder listening on http://%s",
		listener.Addr(),
	)

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve business HTTP: %w", err)

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown business HTTP: %w", err)
		}
		return nil
	}
}
