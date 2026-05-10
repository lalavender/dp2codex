package cache

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

type memEntry struct {
	reasoning string
	expireAt  time.Time
}

type MemCache struct {
	mu       sync.RWMutex
	data     map[string]memEntry
	maxPerSession int
	maxTotal      int
}

func NewMemCache() *MemCache {
	m := &MemCache{
		data:          make(map[string]memEntry),
		maxPerSession: 10,
		maxTotal:      1000,
	}
	go m.cleanupLoop()
	return m
}

func (m *MemCache) key(source, sessionID string) string {
	h := sha256.Sum256([]byte(sessionID))
	return fmt.Sprintf("%s:%x", source, h[:8])
}

func (m *MemCache) Get(source, sessionID string) (string, bool) {
	m.mu.RLock()
	entry, ok := m.data[m.key(source, sessionID)]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expireAt) {
		m.mu.Lock()
		delete(m.data, m.key(source, sessionID))
		m.mu.Unlock()
		return "", false
	}
	return entry.reasoning, true
}

func (m *MemCache) Set(source, sessionID, reasoning string, ttl time.Duration) {
	// 检查总条目数限制
	m.mu.Lock()
	if len(m.data) >= m.maxTotal {
		// 随机删除一个
		for k := range m.data {
			delete(m.data, k)
			break
		}
	}
	m.data[m.key(source, sessionID)] = memEntry{
		reasoning: reasoning,
		expireAt:  time.Now().Add(ttl),
	}
	m.mu.Unlock()
}

func (m *MemCache) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

func (m *MemCache) Close() error { return nil }

func (m *MemCache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for k, v := range m.data {
			if now.After(v.expireAt) {
				delete(m.data, k)
			}
		}
		m.mu.Unlock()
	}
}
