package transport

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"nginx-agent/internal/config"
)

// ServerOption 根据配置返回 gRPC server 的传输凭证选项。
// TLS 关闭时返回明文（仅限开发/内网受信环境）。
func ServerOption(cfg config.TLSConfig) (grpc.ServerOption, error) {
	if !cfg.Enabled {
		return grpc.Creds(insecure.NewCredentials()), nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("加载 agent 证书失败: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CA)
	if err != nil {
		return nil, fmt.Errorf("读取 CA 失败: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("解析 CA 失败")
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		// 要求并校验客户端证书（mTLS）
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  pool,
		MinVersion: tls.VersionTLS12,
	}
	return grpc.Creds(credentials.NewTLS(tlsCfg)), nil
}
