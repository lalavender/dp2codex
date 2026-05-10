package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"
)

// RunConnectTunnel 启动 CONNECT 隧道服务器（端口 8443）
func RunConnectTunnel(port string, targetAddr string) error {
	certFile := "certs/cert.pem"
	keyFile := "certs/key.pem"

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("load tunnel cert: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("tunnel listen: %w", err)
	}
	defer listener.Close()

	slog.Info("CONNECT tunnel server starting", "port", port, "target", targetAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			slog.Warn("tunnel accept error", "error", err)
			continue
		}
		go handleTunnelConn(conn, tlsCfg, targetAddr)
	}
}

func handleTunnelConn(conn net.Conn, tlsCfg *tls.Config, targetAddr string) {
	defer conn.Close()

	// 读取 CONNECT 请求
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	req := string(buf[:n])

	// 解析 CONNECT 目标
	var host string
	if _, err := fmt.Sscanf(req, "CONNECT %s HTTP/", &host); err != nil {
		slog.Warn("invalid CONNECT request")
		return
	}

	// 返回 200
	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// TLS 握手
	tlsConn := tls.Server(conn, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		slog.Warn("tls handshake failed", "error", err)
		return
	}
	defer tlsConn.Close()

	// 连接到本地 HTTP 服务器
	target, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		slog.Warn("tunnel dial target failed", "error", err)
		return
	}
	defer target.Close()

	// 双向管道
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(target, tlsConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(tlsConn, target)
		done <- struct{}{}
	}()
	<-done
}
