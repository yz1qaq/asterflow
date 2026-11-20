package promptcache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCacheKeyedByPromptAndEnv(t *testing.T) {
	cache := NewCache()
	compiled, err := Compile("v1", "hello {{.name}}", `{"required":["name"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get("p", "dev"); ok {
		t.Fatal("empty cache must miss")
	}
	cache.Put("p", "dev", compiled)
	if got, ok := cache.Get("p", "dev"); !ok || got.VersionID != "v1" {
		t.Fatalf("cached = %+v, %v", got, ok)
	}
	if _, ok := cache.Get("p", "prod"); ok {
		t.Fatal("cache must be keyed by environment")
	}
	cache.Invalidate("p", "dev")
	if _, ok := cache.Get("p", "dev"); ok {
		t.Fatal("invalidate must evict")
	}

	var nilCache *Cache
	nilCache.Put("p", "dev", compiled)
	nilCache.Invalidate("p", "dev")
	if _, ok := nilCache.Get("p", "dev"); ok {
		t.Fatal("nil cache must stay empty")
	}
}

func TestParseInvalidateChannel(t *testing.T) {
	for _, tc := range []struct {
		channel, prompt, env string
		ok                   bool
	}{
		{"prompt:p-1:dev:invalidate", "p-1", "dev", true},
		{"prompt:p-1:prod:invalidate", "p-1", "prod", true},
		{"prompt:p-1:dev", "", "", false},
		{"other:p-1:dev:invalidate", "", "", false},
		{"prompt:a:b:dev:invalidate", "", "", false},
	} {
		prompt, env, ok := parseInvalidateChannel(tc.channel)
		if prompt != tc.prompt || env != tc.env || ok != tc.ok {
			t.Fatalf("%s = %q %q %v", tc.channel, prompt, env, ok)
		}
	}
}

func TestSubscriberInvalidatesThroughRedis(t *testing.T) {
	url := os.Getenv("ASTERFLOW_TEST_REDIS_URL")
	if url == "" {
		t.Skip("set ASTERFLOW_TEST_REDIS_URL for the Redis pub/sub subscriber test")
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rds := redis.NewClient(options)
	t.Cleanup(func() { _ = rds.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cache := NewCache()
	compiled, err := Compile("v1", "hello", "{}")
	if err != nil {
		t.Fatal(err)
	}
	cache.Put("p-1", "dev", compiled)
	subCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() { _ = NewSubscriber(rds, cache).Run(subCtx) }()

	channel := InvalidateChannel("p-1", "dev")
	for i := 0; i < 40; i++ {
		count, err := rds.PubSubNumSub(ctx, channel).Result()
		if err == nil && count[channel] > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := NewRedisPublisher(rds).Publish(ctx, channel, "v2"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := cache.Get("p-1", "dev"); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("remote invalidation did not evict the local cache")
}
