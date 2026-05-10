package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config 日志配置
type Config struct {
	Dir      string // 日志目录，空则使用默认路径
	MaxSize  int64  // 单文件最大字节数，默认 10MB
	MaxFiles int    // 最多保留的轮转文件数，默认 5
	MaxAge   int    // 最多保留天数，默认 7
	Debug    bool   // 是否输出 DEBUG 级别
}

const (
	defaultMaxSize  = 10 * 1024 * 1024 // 10MB
	defaultMaxFiles = 5
	defaultMaxAge   = 7
	logFileName     = "dp2codex.log"
)

// Setup 初始化全局日志系统
func Setup(cfg Config) error {
	if cfg.Dir == "" {
		cfg.Dir = defaultLogDir()
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = defaultMaxSize
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = defaultMaxFiles
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = defaultMaxAge
	}

	if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
		return fmt.Errorf("create log dir %s: %w", cfg.Dir, err)
	}

	rw := &rotatingWriter{
		dir:      cfg.Dir,
		maxSize:  cfg.MaxSize,
		maxFiles: cfg.MaxFiles,
		maxAge:   cfg.MaxAge,
	}
	if err := rw.open(); err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	// 双输出: stderr + 文件
	multi := io.MultiWriter(os.Stderr, rw)

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))

	// 启动后台清理
	go rw.cleanupLoop()

	slog.Info("logging initialized", "dir", cfg.Dir, "max_size_mb", cfg.MaxSize/(1024*1024), "max_files", cfg.MaxFiles, "max_age_days", cfg.MaxAge)
	return nil
}

// defaultLogDir 返回平台默认日志目录
func defaultLogDir() string {
	var base string
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(os.Getenv("HOME"), ".dp2codex")
	case "windows":
		base = filepath.Join(os.Getenv("APPDATA"), "dp2codex")
	default:
		base = filepath.Join(os.Getenv("HOME"), ".dp2codex")
	}
	return filepath.Join(base, "logs")
}

// rotatingWriter 带轮转的文件写入器
type rotatingWriter struct {
	mu       sync.Mutex
	dir      string
	maxSize  int64
	maxFiles int
	maxAge   int
	file     *os.File
	size     int64
}

func (w *rotatingWriter) open() error {
	path := filepath.Join(w.dir, logFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			// 轮转失败仍尝试写入
			fmt.Fprintf(os.Stderr, "log rotate error: %v\n", err)
		}
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return
}

func (w *rotatingWriter) rotate() error {
	if w.file != nil {
		w.file.Close()
	}

	base := filepath.Join(w.dir, logFileName)

	// 移动现有文件: .4→.5, .3→.4, ..., current→.1
	for i := w.maxFiles - 1; i >= 1; i-- {
		src := base + fmt.Sprintf(".%d", i)
		dst := base + fmt.Sprintf(".%d", i+1)
		os.Rename(src, dst)
	}
	os.Rename(base, base+".1")

	// 删除超出 maxFiles 的文件
	excess := base + fmt.Sprintf(".%d", w.maxFiles+1)
	os.Remove(excess)

	// 打开新文件
	f, err := os.OpenFile(base, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0
	return nil
}

// cleanupLoop 每小时检查并清理过期日志
func (w *rotatingWriter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// 启动时立即清理一次
	w.cleanupOld()

	for range ticker.C {
		w.cleanupOld()
	}
}

func (w *rotatingWriter) cleanupOld() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-time.Duration(w.maxAge) * 24 * time.Hour)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), logFileName) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(w.dir, entry.Name())
			if err := os.Remove(path); err == nil {
				slog.Info("log retention: removed expired file", "path", path, "max_age_days", w.maxAge)
			}
		}
	}
}

// LogDir 返回当前使用的日志目录（供启动横幅显示）
func LogDir(dir string) string {
	if dir != "" {
		return dir
	}
	return defaultLogDir()
}

// SetupSimple 仅设置 stderr 输出（不写文件，用于调试）
func SetupSimple(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

// Handler 返回可附加 context 的 slog handler（预留扩展）
type Handler struct {
	slog.Handler
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	return h.Handler.Handle(ctx, r)
}

// ListLogFiles 返回日志目录中的所有日志文件信息
func ListLogFiles(dir string) []FileInfo {
	if dir == "" {
		dir = defaultLogDir()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []FileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), logFileName) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files
}

type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}
