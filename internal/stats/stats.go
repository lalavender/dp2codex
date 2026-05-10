package stats

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var reReplace = regexp.MustCompile(`sk-[A-Za-z0-9]{32,}|(?i)Bearer\s+[A-Za-z0-9._-]+`)

type Stats struct {
	mu               sync.RWMutex
	startTime        time.Time
	totalRequests    atomic.Int64
	activeStreams    atomic.Int64
	errorsByCode     map[int]int64
	cacheHits        atomic.Int64
	cacheMisses      atomic.Int64
	upstreamErrors   map[string]int64
	upstreamErrOrder []string
	logBuf           []LogEntry
	logMax           int
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

var globalStats = &Stats{
	startTime:      time.Now(),
	errorsByCode:   make(map[int]int64),
	upstreamErrors: make(map[string]int64),
	logBuf:         make([]LogEntry, 0, 200),
	logMax:         200,
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

func RecordRequest() { globalStats.totalRequests.Add(1) }

func RecordCache(hit bool) {
	if hit {
		globalStats.cacheHits.Add(1)
	} else {
		globalStats.cacheMisses.Add(1)
	}
}

func RecordError(code int) {
	globalStats.mu.Lock()
	globalStats.errorsByCode[code]++
	globalStats.mu.Unlock()
}

func RecordUpstreamError(msg string) {
	globalStats.mu.Lock()
	globalStats.upstreamErrors[msg]++
	if len(globalStats.upstreamErrOrder) < 5 {
		globalStats.upstreamErrOrder = append(globalStats.upstreamErrOrder, msg)
	}
	globalStats.mu.Unlock()
}

func IncrementActiveStreams() { globalStats.activeStreams.Add(1) }
func DecrementActiveStreams() { globalStats.activeStreams.Add(-1) }

func Sanitize(msg string) string {
	return reReplace.ReplaceAllString(msg, "***")
}

func Log(format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	msg = Sanitize(msg)
	slog.Info(msg)

	entry := LogEntry{Time: time.Now(), Message: msg}
	globalStats.mu.Lock()
	if len(globalStats.logBuf) >= globalStats.logMax {
		globalStats.logBuf = globalStats.logBuf[1:]
	}
	globalStats.logBuf = append(globalStats.logBuf, entry)
	globalStats.mu.Unlock()
}

func GetLogs(limit int) []LogEntry {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()
	n := limit
	if n > len(globalStats.logBuf) {
		n = len(globalStats.logBuf)
	}
	result := make([]LogEntry, n)
	copy(result, globalStats.logBuf[len(globalStats.logBuf)-n:])
	return result
}

func GetStats() map[string]any {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()

	errs := make(map[string]int64)
	for code, count := range globalStats.errorsByCode {
		errs[strconv.Itoa(code)] = count
	}

	total := globalStats.totalRequests.Load()
	var errTotal int64
	for _, v := range globalStats.errorsByCode {
		errTotal += v
	}
	var errRate float64
	if total > 0 {
		errRate = float64(errTotal) / float64(total) * 100
	}

	hits := globalStats.cacheHits.Load()
	misses := globalStats.cacheMisses.Load()
	var cacheRate float64
	if hits+misses > 0 {
		cacheRate = float64(hits) / float64(hits+misses) * 100
	}

	upstream := make(map[string]int64)
	for _, k := range globalStats.upstreamErrOrder {
		upstream[k] = globalStats.upstreamErrors[k]
	}

	return map[string]any{
		"uptime_seconds":  time.Since(globalStats.startTime).Seconds(),
		"total_requests":  total,
		"active_streams":  globalStats.activeStreams.Load(),
		"errors_by_code":  errs,
		"error_rate":      errRate,
		"cache_hits":      hits,
		"cache_misses":    misses,
		"cache_hit_rate":  cacheRate,
		"upstream_errors": upstream,
	}
}
