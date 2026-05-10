package cache

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
	prefix string
}

func NewRedisCache(addr string, prefix string) *RedisCache {
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	r := &RedisCache{client: rdb, prefix: prefix}
	// 检查连接
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Warn("redis connection failed, falling back to memory", "error", err)
		return nil
	}
	go r.healthLoop()
	return r
}

func (r *RedisCache) key(source, sessionID string) string {
	return r.prefix + source + ":" + sessionID
}

func (r *RedisCache) Get(source, sessionID string) (string, bool) {
	if r.client == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	val, err := r.client.Get(ctx, r.key(source, sessionID)).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func (r *RedisCache) Set(source, sessionID, reasoning string, ttl time.Duration) {
	if r.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r.client.Set(ctx, r.key(source, sessionID), reasoning, ttl)
}

func (r *RedisCache) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

func (r *RedisCache) healthLoop() {
	ticker := time.NewTicker(60 * time.Second)
	for range ticker.C {
		if r.client == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.client.Ping(ctx).Err(); err != nil {
			slog.Warn("redis health check failed", "error", err)
		}
	}
}
