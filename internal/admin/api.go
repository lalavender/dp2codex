package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"dp2codex/internal/cache"
	"dp2codex/internal/config"
	"dp2codex/internal/stats"
)

// AdminAPI 管理 API 处理器
type AdminAPI struct {
	reasoningCache *cache.MemCache
}

func NewAdminAPI(rc *cache.MemCache) *AdminAPI {
	return &AdminAPI{reasoningCache: rc}
}

func (a *AdminAPI) GetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	cfg := config.Global.ConfigDict()
	json.NewEncoder(w).Encode(cfg)
}

func (a *AdminAPI) SetConfig(w http.ResponseWriter, r *http.Request) {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, `{"error":"invalid json"}`, 400)
		return
	}
	errs := config.Global.Update(updates)
	if len(errs) > 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "partial",
			"errors": errs,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *AdminAPI) GetStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats.GetStats())
}

func (a *AdminAPI) GetSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	count := 0
	if a.reasoningCache != nil {
		count = a.reasoningCache.SessionCount()
	}
	json.NewEncoder(w).Encode(map[string]any{
		"memory_sessions": count,
		"redis_sessions":  0,
	})
}

func (a *AdminAPI) GetLogs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"logs": stats.GetLogs(limit),
	})
}

func (a *AdminAPI) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "1.0.0",
	})
}
