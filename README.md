# nginx-agent

Nginx 可视化管理平台的**节点代理**（Agent 端）。以 systemd 守护进程运行在每台 Nginx 主机上，
通过 gRPC（over mTLS）接受中心 `nginx-admin` 调度，负责本地配置读写、`nginx -t`、reload、备份。

## 目录结构

```
nginx-agent/
├── api/proto/agent.proto         # gRPC 接口定义（与 nginx-admin 同步）
├── cmd/nginx-agent/main.go       # 程序入口
├── internal/
│   ├── config/                   # 配置加载（config.yaml）
│   ├── pb/                       # protoc 生成代码
│   ├── fsops/                    # 安全文件读写（路径白名单 + 命令黑名单 + 乐观锁）
│   ├── nginxctl/                 # nginx -t / reload / 状态采集
│   ├── snapshot/                 # 本地快照备份 + 保留份数 + 回滚内容
│   ├── discover/                 # 配置发现（nginx.conf / conf.d / sites-enabled）
│   └── server/                   # gRPC server，实现 AgentService（含安全闭环）
├── config.yaml                   # 配置示例
└── Makefile
```

## 部署（按你的目录约定）

二进制与配置放在项目目录下，例如 `/data/nginx-agent/`：

```
/data/nginx-agent/nginx-agent      # 二进制
/data/nginx-agent/config.yaml      # 配置
/data/nginx-agent/backups/         # 本地快照
/data/nginx-agent/tls/             # mTLS 证书（生产）
```

运行：

```bash
/data/nginx-agent/nginx-agent -config /data/nginx-agent/config.yaml
```

systemd 单元示例：

```ini
[Unit]
Description=nginx-agent
After=network.target nginx.service

[Service]
ExecStart=/data/nginx-agent/nginx-agent -config /data/nginx-agent/config.yaml
WorkingDirectory=/data/nginx-agent
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## 安全闭环

`WriteConfig` 执行：写入前快照 → 写入 → `nginx -t` 校验 → 通过则 reload；
任一步失败自动回滚到快照。路径白名单 + 高危命令黑名单 + 默认禁改主配置 `nginx.conf`。

## 开发

```bash
make proto    # 重新生成 protobuf 代码（需 protoc + 插件）
make build    # 构建到 bin/nginx-agent
make run      # 本地运行
make vet
```

> 注：依赖固定在与 Go 1.23 兼容的版本（grpc v1.67.1、protobuf v1.35.1 等）。
