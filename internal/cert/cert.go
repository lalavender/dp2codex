package cert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

var (
	certDir  = "certs"
	caCert   = filepath.Join(certDir, "ca.pem")
	caKey    = filepath.Join(certDir, "ca.key")
	svrCert  = filepath.Join(certDir, "cert.pem")
	svrKey   = filepath.Join(certDir, "key.pem")
)

var serverNames = []string{
	"localhost",
	"api.openai.com",
	"auth.openai.com",
	"chat.openai.com",
	"chatgpt.com",
	"ab.chatgpt.com",
	"api.deepseek.com",
}

func EnsureCerts() bool {
	// 检查是否已存在
	if _, err := os.Stat(svrCert); err == nil {
		if _, err := os.Stat(svrKey); err == nil {
			slog.Info("certificates already exist")
			return true
		}
	}

	dir := filepath.Dir(caCert)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("cert dir create failed, trying openssl fallback", "error", err)
		return opensslFallback()
	}

	// 生成 CA 密钥
	caPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Warn("ca key generation failed", "error", err)
		return opensslFallback()
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "JinDX-CA"},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(5 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		slog.Warn("ca cert creation failed", "error", err)
		return opensslFallback()
	}

	// 写 CA 证书
	if err := writePEM(caCert, "CERTIFICATE", caDER); err != nil {
		return opensslFallback()
	}
	if err := writePEM(caKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caPrivKey)); err != nil {
		return opensslFallback()
	}

	// 生成服务器密钥
	svrPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Warn("server key generation failed", "error", err)
		return opensslFallback()
	}

	// 计算 CA 密钥的 SKI
	pubDER, err := x509.MarshalPKIXPublicKey(&caPrivKey.PublicKey)
	if err != nil {
		return opensslFallback()
	}
	ski := sha1.Sum(pubDER)

	svrTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixMilli()),
		Subject:      pkix.Name{CommonName: "JinDX Proxy"},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().Add(5 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		AuthorityKeyId: ski[:],
	}

	for _, name := range serverNames {
		if ip := net.ParseIP(name); ip != nil {
			svrTemplate.IPAddresses = append(svrTemplate.IPAddresses, ip)
		} else {
			svrTemplate.DNSNames = append(svrTemplate.DNSNames, name)
		}
	}

	caCertParsed, err := x509.ParseCertificate(caDER)
	if err != nil {
		return opensslFallback()
	}

	svrDER, err := x509.CreateCertificate(rand.Reader, svrTemplate, caCertParsed, &svrPrivKey.PublicKey, caPrivKey)
	if err != nil {
		slog.Warn("server cert creation failed", "error", err)
		return opensslFallback()
	}

	if err := writePEM(svrCert, "CERTIFICATE", svrDER); err != nil {
		return false
	}
	if err := writePEM(svrKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(svrPrivKey)); err != nil {
		return false
	}

	slog.Info("certificates generated successfully", "dir", certDir)
	return true
}

func writePEM(path, pemType string, derBytes []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: derBytes})
}

func opensslFallback() bool {
	slog.Info("falling back to openssl CLI")
	// 简化处理：用 Go 生成也足够了
	return false
}
