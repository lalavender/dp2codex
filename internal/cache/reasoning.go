package cache

import (
	"time"
)

type ReasoningCache struct {
	mem           *MemCache
	maxPerSession int
}

func NewReasoningCache() *ReasoningCache {
	return &ReasoningCache{
		mem:           NewMemCache(),
		maxPerSession: 10,
	}
}

func (c *ReasoningCache) Get(source, sessionID string, ttl time.Duration) ([]string, bool) {
	return c.mem.Get(source, sessionID, ttl)
}

func (c *ReasoningCache) Set(source, sessionID, reasoning string, ttl time.Duration) {
	if reasoning == "" {
		return
	}
	c.mem.Set(source, sessionID, reasoning)
}
