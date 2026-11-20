package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yz1qaq/asterflow/internal/api"
	"github.com/yz1qaq/asterflow/internal/config"
	"github.com/yz1qaq/asterflow/internal/infrastructure/postgres"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
	"github.com/yz1qaq/asterflow/internal/runtime/promptcache"
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

	credentialCipher, err := modelconfig.NewCipher(
		[]byte(cfg.CredentialSecretKey),
	)
	if err != nil {
		return fmt.Errorf("initialize credential cipher: %w", err)
	}

	modelStore := modelconfig.NewPostgresStore(pool)
	modelService := modelconfig.NewService(modelStore, credentialCipher)

	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("parse Redis URL: %w", err)
	}
	redisClient := redis.NewClient(redisOptions)
	defer redisClient.Close()
	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	err = redisClient.Ping(pingCtx).Err()
	cancelPing()
	if err != nil {
		return fmt.Errorf("connect Redis: %w", err)
	}

	promptStore := prompt.NewPostgresStore(pool)
	promptCache := promptcache.NewCache()
	promptService := prompt.NewServiceWithCache(
		promptStore,
		modelService,
		promptCache,
		promptcache.NewRedisPublisher(redisClient),
	)
	go func() {
		if err := promptcache.NewSubscriber(redisClient, promptCache).Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("prompt cache subscriber stopped: %v", err)
		}
	}()

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

	handler := api.NewRouter(pool, iamService, modelService, promptService)
	log.Printf("starting HTTP server on %s", cfg.HTTPAddr)

	return run(ctx, handler, cfg.HTTPAddr)
}
