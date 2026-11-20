package promptcache

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// InvalidateChannel is the per prompt@env channel used to broadcast pointer changes.
func InvalidateChannel(promptID, env string) string {
	return fmt.Sprintf("prompt:%s:%s:invalidate", promptID, env)
}

// RedisPublisher broadcasts invalidation messages to all runtime processes.
type RedisPublisher struct {
	rds *redis.Client
}

func NewRedisPublisher(rds *redis.Client) *RedisPublisher {
	return &RedisPublisher{rds: rds}
}

func (p *RedisPublisher) Publish(ctx context.Context, channel, message string) error {
	return p.rds.Publish(ctx, channel, message).Err()
}

// Subscriber listens for invalidation messages and evicts the local cache.
type Subscriber struct {
	rds   *redis.Client
	cache *Cache
}

func NewSubscriber(rds *redis.Client, cache *Cache) *Subscriber {
	return &Subscriber{rds: rds, cache: cache}
}

// Run blocks until ctx is canceled or the subscription closes.
func (s *Subscriber) Run(ctx context.Context) error {
	pubsub := s.rds.PSubscribe(ctx, "prompt:*:invalidate")
	defer pubsub.Close()

	channel := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-channel:
			if !ok {
				return nil
			}
			promptID, env, parsed := parseInvalidateChannel(message.Channel)
			if parsed {
				s.cache.Invalidate(promptID, env)
			}
		}
	}
}

func parseInvalidateChannel(channel string) (string, string, bool) {
	parts := strings.Split(channel, ":")
	if len(parts) != 4 || parts[0] != "prompt" || parts[3] != "invalidate" {
		return "", "", false
	}
	return parts[1], parts[2], true
}
