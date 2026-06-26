package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"nginx-agent/internal/config"
	"nginx-agent/internal/pb"
	"nginx-agent/internal/server"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "配置文件路径")
	showVersion := flag.Bool("version", false, "打印版本并退出")
	flag.Parse()

	if *showVersion {
		log.Printf("nginx-agent %s", server.Version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	agentSrv, err := server.NewAgentServer(cfg)
	if err != nil {
		log.Fatalf("初始化 agent 失败: %v", err)
	}

	creds, err := server.TransportOption(cfg.Agent.TLS)
	if err != nil {
		log.Fatalf("初始化传输凭证失败: %v", err)
	}

	lis, err := net.Listen("tcp", cfg.Agent.Listen)
	if err != nil {
		log.Fatalf("监听 %s 失败: %v", cfg.Agent.Listen, err)
	}

	grpcServer := grpc.NewServer(creds)
	pb.RegisterAgentServiceServer(grpcServer, agentSrv)

	// 优雅退出
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Printf("收到退出信号，正在关闭...")
		grpcServer.GracefulStop()
	}()

	mode := "明文(开发)"
	if cfg.Agent.TLS.Enabled {
		mode = "mTLS"
	}
	log.Printf("nginx-agent %s 启动: 监听 %s [%s], config_root=%s",
		server.Version, cfg.Agent.Listen, mode, cfg.Nginx.ConfigRoot)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC 服务退出: %v", err)
	}
}
