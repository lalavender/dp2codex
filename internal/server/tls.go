package server

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// NewTLSServer 创建 HTTPS 服务器（端口 8444）
func NewTLSServer(port string, handler http.Handler) *http.Server {
	certFile := "certs/cert.pem"
	keyFile := "certs/key.pem"

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		slog.Warn("TLS cert not found, HTTPS server not available", "cert", certFile)
		return nil
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		slog.Warn("TLS key not found, HTTPS server not available", "key", keyFile)
		return nil
	}

	slog.Info("TLS server starting", "port", port)
	return &http.Server{
		Addr:      ":" + port,
		Handler:   handler,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}
}

// ServeTLS 启动 HTTPS 监听（包装 http.Server.ListenAndServeTLS）
func ServeTLS(srv *http.Server) error {
	return srv.ListenAndServeTLS("certs/cert.pem", "certs/key.pem")
}
