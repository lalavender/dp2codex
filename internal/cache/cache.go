package cache

import "time"

type Cache interface {
	Get(source, sessionID string) (string, bool)
	Set(source, sessionID, reasoning string, ttl time.Duration)
	Close() error
}
