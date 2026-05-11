package cache

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

type memEntry struct {
	text string
	ts   time.Time
}

type MemCache struct {
	mu sync.RWMutex
	// key: source:hash(sessionID) -> entries
	data map[string][]memEntry

	maxPerSession int
	maxTotal      int
}

func NewMemCache() *MemCache {
	m := &MemCache{
		data:          make(map[string][]memEntry),
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

func (m *MemCache) Get(source, sessionID string, ttl time.Duration) ([]string, bool) {
	k := m.key(source, sessionID)
	now := time.Now()

	m.mu.Lock()
	entries, ok := m.data[k]
	if !ok || len(entries) == 0 {
		m.mu.Unlock()
		return nil, false
	}

	// 过滤过期
	valid := entries[:0]
	for _, e := range entries {
		if now.Sub(e.ts) < ttl {
			valid = append(valid, e)
		}
	}
	if len(valid) == 0 {
		delete(m.data, k)
		m.mu.Unlock()
		return nil, false
	}
	// 写回过滤后的
	m.data[k] = valid
	m.mu.Unlock()

	out := make([]string, 0, len(valid))
	for _, e := range valid {
		out = append(out, e.text)
	}
	return out, true
}

func (m *MemCache) Set(source, sessionID, reasoning string) {
	if reasoning == "" {
		return
	}
	k := m.key(source, sessionID)

	m.mu.Lock()
	// 检查总会话数限制
	if len(m.data) >= m.maxTotal {
		// 随机删除一个会话
		for kk := range m.data {
			delete(m.data, kk)
			break
		}
	}

	entries := m.data[k]
	entries = append(entries, memEntry{text: reasoning, ts: time.Now()})
	if len(entries) > m.maxPerSession {
		entries = entries[len(entries)-m.maxPerSession:]
	}
	m.data[k] = entries
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
		// 由调用方传入 ttl 做精确过滤；这里仅做上限保护（防止长期无人访问导致不清理）
		m.mu.Lock()
		if len(m.data) > m.maxTotal {
			for kk := range m.data {
				delete(m.data, kk)
				if len(m.data) <= m.maxTotal {
					break
				}
			}
		}
		m.mu.Unlock()
	}
}
