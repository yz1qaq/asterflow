package prompt

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/yz1qaq/asterflow/internal/runtime/promptcache"
)

type capturePublisher struct {
	mu    sync.Mutex
	items []struct{ channel, message string }
}

func (p *capturePublisher) Publish(_ context.Context, channel, message string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = append(p.items, struct{ channel, message string }{channel, message})
	return nil
}

func (p *capturePublisher) snapshot() []struct{ channel, message string } {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]struct{ channel, message string }{}, p.items...)
}

func TestPromptServiceCache(t *testing.T) {
	pool := newPromptTestPool(t)
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	applyPromptMigration(t, pool, "000007_prompt_experiments.up.sql")
	applyPromptMigration(t, pool, "000008_prompt_env_events.up.sql")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	pub := &capturePublisher{}
	cache := promptcache.NewCache()
	s := NewServiceWithCache(store, nil, cache, pub)

	p, v1, err := s.CreatePrompt(ctx, CreatePromptRequest{WorkspaceID: "space-a", Name: "Orders",
		VersionInput: VersionInput{Template: "old {{.id}}"}})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.CreateVersion(ctx, p.ID, CreateVersionRequest{WorkspaceID: "space-a",
		VersionInput: VersionInput{Template: "new {{.id}}"}})
	if err != nil {
		t.Fatal(err)
	}

	if text, err := s.RenderByEnv(ctx, "space-a", p.ID, "dev", "user-1", map[string]any{"id": "1"}); err != nil || text != "old 1" {
		t.Fatalf("initial render = %q, %v", text, err)
	}
	if compiled, ok := cache.Get(p.ID, "dev"); !ok || compiled.VersionID != v1.ID {
		t.Fatalf("baseline render must populate the cache: %+v, %v", compiled, ok)
	}

	if err := s.Promote(ctx, "space-a", p.ID, "dev", v2.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if compiled, ok := cache.Get(p.ID, "dev"); !ok || compiled.VersionID != v2.ID {
		t.Fatalf("promote must refresh the cache: %+v, %v", compiled, ok)
	}
	if messages := pub.snapshot(); len(messages) != 1 || messages[0].channel != promptcache.InvalidateChannel(p.ID, "dev") || messages[0].message != v2.ID {
		t.Fatalf("promote broadcast = %+v", messages)
	}

	if _, err := pool.Exec(ctx, "UPDATE prompt_versions SET template='hacked {{.id}}' WHERE id=$1", uuid.MustParse(v2.ID)); err != nil {
		t.Fatal(err)
	}
	if text, err := s.RenderByEnv(ctx, "space-a", p.ID, "dev", "user-1", map[string]any{"id": "1"}); err != nil || text != "new 1" {
		t.Fatalf("cached render = %q, %v", text, err)
	}
	cache.Invalidate(p.ID, "dev")
	if text, err := s.RenderByEnv(ctx, "space-a", p.ID, "dev", "user-1", map[string]any{"id": "1"}); err != nil || text != "hacked 1" {
		t.Fatalf("render after invalidation = %q, %v", text, err)
	}

	if err := s.Rollback(ctx, "space-a", p.ID, "dev", v1.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if compiled, ok := cache.Get(p.ID, "dev"); !ok || compiled.VersionID != v1.ID {
		t.Fatalf("rollback must refresh the cache: %+v, %v", compiled, ok)
	}
	if messages := pub.snapshot(); len(messages) != 2 || messages[1].message != v1.ID {
		t.Fatalf("rollback broadcast = %+v", messages)
	}

	if _, err := s.StartRollout(ctx, "space-a", p.ID, "dev", v2.ID, "owner", 20); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteRollout(ctx, "space-a", p.ID, "dev", "owner"); err != nil {
		t.Fatal(err)
	}
	if compiled, ok := cache.Get(p.ID, "dev"); !ok || compiled.VersionID != v2.ID {
		t.Fatalf("completion must refresh the cache: %+v, %v", compiled, ok)
	}
	if messages := pub.snapshot(); len(messages) != 3 || messages[2].message != v2.ID {
		t.Fatalf("completion broadcast = %+v", messages)
	}
}

func TestPromptCacheInvalidationThroughRedis(t *testing.T) {
	redisURL := os.Getenv("ASTERFLOW_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("set ASTERFLOW_TEST_REDIS_URL for cross-process invalidation")
	}
	pool := newPromptTestPool(t)
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	applyPromptMigration(t, pool, "000007_prompt_experiments.up.sql")
	applyPromptMigration(t, pool, "000008_prompt_env_events.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A')"); err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rds := redis.NewClient(options)
	t.Cleanup(func() { _ = rds.Close() })

	store := NewPostgresStore(pool)
	s := NewServiceWithCache(store, nil, promptcache.NewCache(), promptcache.NewRedisPublisher(rds))
	p, _, err := s.CreatePrompt(ctx, CreatePromptRequest{WorkspaceID: "space-a", Name: "Orders",
		VersionInput: VersionInput{Template: "old {{.id}}"}})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.CreateVersion(ctx, p.ID, CreateVersionRequest{WorkspaceID: "space-a",
		VersionInput: VersionInput{Template: "new {{.id}}"}})
	if err != nil {
		t.Fatal(err)
	}

	otherProcess := promptcache.NewCache()
	stale, err := promptcache.Compile("stale", "stale", "{}")
	if err != nil {
		t.Fatal(err)
	}
	otherProcess.Put(p.ID, "dev", stale)
	subCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() { _ = promptcache.NewSubscriber(rds, otherProcess).Run(subCtx) }()

	channel := promptcache.InvalidateChannel(p.ID, "dev")
	for i := 0; i < 40; i++ {
		count, err := rds.PubSubNumSub(ctx, channel).Result()
		if err == nil && count[channel] > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := s.Promote(ctx, "space-a", p.ID, "dev", v2.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := otherProcess.Get(p.ID, "dev"); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("other process cache was not invalidated through Redis")
}
