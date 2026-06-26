# nginx-agent

Nginx 可视化管理平台的**节点代理**（Agent 端）。以 systemd 守护进程运行在每台 Nginx 主机上，
通过 gRPC（over mTLS）接受中心 `nginx-admin` 调度，负责本地配置读写、`nginx -t`、reload、备份。

> 配套中心控制台：[nginx-admin](https://github.com/Nginx-Admin/nginx-admin)（全平台部署一个）。

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
│   └── server/                   # gRPC server，实现 AgentService（含安全闭环 + mTLS）
├── deploy/
│   └── nginx-agent.service       # systemd 单元
├── config.yaml                   # 配置示例
└── Makefile
```

## 依赖与前置

- Go 1.26.2；目标机已安装 nginx。
- 纯 Go 依赖（grpc / protobuf / yaml），**无 CGO**，可静态交叉编译。

## 编译

本机编译：

```bash
go build -o nginx-agent ./cmd/nginx-agent
```

交叉编译到 Linux 服务器（推荐静态编译，不挑发行版/glibc 版本）：

```bash
# Ubuntu 24 / x86_64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o nginx-agent ./cmd/nginx-agent

# ARM64（如 Graviton）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o nginx-agent ./cmd/nginx-agent
```

> 目标机架构不确定时，`uname -m`：`x86_64` → amd64，`aarch64` → arm64。

## 部署（按目录约定，systemd）

二进制与配置放在项目目录下，例如 `/data/nginx-agent/`：

```bash
mkdir -p /data/nginx-agent/backups /data/nginx-agent/tls
install nginx-agent  /data/nginx-agent/nginx-agent
install config.yaml  /data/nginx-agent/config.yaml          # 按本机实际路径修改
install -D deploy/nginx-agent.service /usr/lib/systemd/system/nginx-agent.service

systemctl daemon-reload
systemctl enable --now nginx-agent
systemctl status nginx-agent
journalctl -u nginx-agent -f
```

目录布局：

```
/data/nginx-agent/nginx-agent      # 二进制
/data/nginx-agent/config.yaml      # 配置
/data/nginx-agent/backups/         # 本地快照
/data/nginx-agent/tls/             # mTLS 证书（生产）
```

> Agent 需执行 `systemctl reload nginx`、读写 `/etc/nginx`，默认以 **root** 运行。
> 默认监听 `0.0.0.0:7443` 的 gRPC，仅内网/防火墙放行；中心去连 Agent（Agent 不主动连中心）。

## 配置要点（config.yaml）

- `nginx.config_root` / `binary` / `test_cmd` / `reload_cmd`：吸收本机目录与命令差异（中心无需感知）。
- `nginx.allowed_paths`：路径白名单，只允许读写声明目录。
- `nginx.allow_main_config`：默认 `false`，禁止改写主配置 `nginx.conf`（高危）。
- `agent.tls.enabled`：开发期可关，生产务必开启 mTLS。
- `backup.dir` / `retain`：本地快照目录与保留份数。

## 安全闭环

`WriteConfig` 执行：写入前快照 → 写入 → `nginx -t` 校验 → 通过则 reload；
任一步失败自动回滚到快照。叠加路径白名单 + 高危命令黑名单 + 默认禁改主配置。

## 开发

```bash
make proto    # 重新生成 protobuf 代码（需 protoc + 插件）
make build    # 构建到 bin/nginx-agent
make run      # 本地运行
make vet
```

> 后端 Go 1.26.2；依赖：grpc v1.67.1、protobuf v1.35.1、yaml.v3 等。

## 许可证

MIT

