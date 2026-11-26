package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yz1qaq/asterflow/internal/api"
	"github.com/yz1qaq/asterflow/internal/config"
	"github.com/yz1qaq/asterflow/internal/infrastructure/postgres"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
	"github.com/yz1qaq/asterflow/internal/platform/conversation"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
	"github.com/yz1qaq/asterflow/internal/runtime/promptcache"
	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
)

//程序的总入口,初始化组件、连接数据库、启动http服务

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

	//连接数据库
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

	agentStore := agent.NewPostgresStore(pool)
	agentService := agent.NewService(agentStore, promptService, modelService)

	gateway := llm.NewFailoverGateway(modelService, llm.NewEinoOpenAIClient())
	executor := engine.NewExecutor(promptService, gateway)

	conversationStore := conversation.NewPostgresStore(pool)
	conversationService := conversation.NewService(conversationStore, agentService, promptService, executor)

	toolStore := tool.NewPostgresStore(pool)
	toolService := tool.NewService(toolStore)
	agentService.ConfigureToolResolver(toolService)

	// 只为显式指定的主机提供私网例外。
	endpointPolicy := tool.NewEndpointPolicy(strings.Split(cfg.ToolAllowedHosts, ","), false)
	toolService.ConfigureSecurity(credentialCipher, endpointPolicy)
	toolRegistry, err := tooling.NewRegistry(endpointPolicy)
	if err != nil {
		return fmt.Errorf("initialize tool registry: %w", err)
	}
	toolRegistry.ConfigureVersionLoader(toolService)
	executor.ConfigureTools(toolRegistry)

	handler := api.NewRouter(pool, iamService, modelService, promptService, api.Services{
		Agent:        agentService,
		Conversation: conversationService,
		Tool:         toolService,
		ToolRegistry: toolRegistry,
	})

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

	log.Printf("starting HTTP server on %s", cfg.HTTPAddr)

	return run(ctx, handler, cfg.HTTPAddr)
}
