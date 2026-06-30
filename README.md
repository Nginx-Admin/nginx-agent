# nginx-agent

Nginx 可视化管理平台的**节点代理**（Agent 端）。以 systemd 守护进程运行在每台 Nginx 主机上，通过 gRPC（可选 mTLS）接受中心 [nginx-admin](https://github.com/Nginx-Admin/nginx-admin) 调度，负责本地配置发现/读写、`nginx -t` 校验、reload 与快照备份/回滚。

> 配套中心控制台：[nginx-admin](https://github.com/Nginx-Admin/nginx-admin)（全平台部署一个）。  
> 详细部署步骤见 [deploy/README.md](deploy/README.md)。

## 架构定位

```
┌─────────────────┐         gRPC (mTLS)          ┌──────────────────┐
│   nginx-admin   │ ───────────────────────────► │   nginx-agent    │
│  （中心控制台）   │      中心主动连接 Agent        │ （每台 nginx 主机）│
└─────────────────┘                              └────────┬─────────┘
                                                            │
                                                   读写 /etc/nginx
                                                   nginx -t / reload
                                                   本地快照 backups/
```

- **连接方向**：中心去连 Agent（Agent 不主动连中心），默认监听 `0.0.0.0:7443`。
- **路径抽象**：中心只传**逻辑路径**（相对 `config_root`），本机目录差异由 Agent 的 `config.yaml` 吸收。
- **权限模型**：Agent 需 root 运行（读写 `/etc/nginx`、执行 `systemctl reload nginx` 等）。

## 功能概览

| 能力 | 说明 |
|------|------|
| 健康与状态 | Ping、采集 nginx 运行状态/版本/PID、最近一次 `nginx -t` 结果 |
| 配置发现 | 递归解析 `include`，兼容 OpenResty 等非标准布局；提取 `server_name` 列表 |
| 配置读写 | 白名单路径内读写；乐观锁（checksum）；原子写入（临时文件 + rename） |
| 安全闭环 | 写入前快照 → 写入 → `nginx -t` → reload；任一步失败自动回滚 |
| 备份/回滚 | 本地快照（默认保留 50 份）；支持按 `backup_ref` 回滚 |
| 命令安全 | `test_cmd` / `reload_cmd` 命中高危黑名单则拒绝执行 |

## 目录结构

```
nginx-agent/
├── api/proto/agent.proto         # gRPC 接口定义（与 nginx-admin 同步）
├── cmd/nginx-agent/main.go       # 程序入口（-config / -version）
├── internal/
│   ├── config/                   # 配置加载（config.yaml）
│   ├── pb/                       # protoc 生成代码
│   ├── fsops/                    # 安全文件读写（路径白名单 + 乐观锁 + 原子写）
│   ├── nginxctl/                 # nginx -t / reload / 状态采集
│   ├── snapshot/                 # 本地快照备份 + 保留份数裁剪
│   ├── discover/                 # 配置发现（include 递归 + 常见目录兜底）
│   └── server/                   # gRPC server，实现 AgentService
├── deploy/
│   ├── nginx-agent.service       # systemd 单元
│   ├── install.sh                # 一键安装脚本
│   └── README.md                 # 部署文档
├── config.yaml                   # 配置示例
└── Makefile
```

## gRPC 接口（AgentService）

| RPC | 说明 |
|-----|------|
| `Ping` | 返回 Agent 版本与时间戳 |
| `GetStatus` | nginx 运行状态、版本、master PID、config_root、最近 test 结果 |
| `DiscoverConfigs` | 扫描本机配置，返回文件列表 + `server_name` |
| `ListConfigs` | 配置文件列表（含 size/mtime/checksum） |
| `ReadConfig` | 读取指定逻辑路径内容 |
| `WriteConfig` | 安全闭环写入（支持 `auto_backup`、`expected_checksum` 乐观锁） |
| `DeleteConfig` | 安全闭环删除（快照 → 删除 → nginx -t → reload；默认禁止删主配置） |
| `TestConfig` | 执行 `nginx -t` |
| `Reload` | 执行 reload |
| `ListBackups` | 列出本地快照（可按 `logical_path` 过滤） |
| `Rollback` | 回滚到指定快照（回滚前再快照一次，便于"回滚的回滚"） |
| `GetAgentSettings` | **只读**返回当前本地配置（`backup.retain`、`allow_main_config`） |
| `UpdateAgentSettings` | **已下线**，请直接改 `config.yaml` 并重启 Agent |

## 依赖与前置

- Go 1.26.2；目标机已安装 nginx。
- 纯 Go 依赖（grpc / protobuf / yaml），**无 CGO**，可静态交叉编译。

## 编译

本机编译：

```bash
go build -o nginx-agent ./cmd/nginx-agent
```

交叉编译到 Linux 服务器（推荐静态编译）：

```bash
# x86_64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o nginx-agent ./cmd/nginx-agent

# ARM64（如 Graviton）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o nginx-agent ./cmd/nginx-agent
```

> 目标机架构不确定时：`uname -m` 中 `x86_64` → amd64，`aarch64` → arm64。

## 部署（systemd）

推荐目录 `/data/nginx-agent/`：

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

或使用 `deploy/install.sh` 一键安装，详见 [deploy/README.md](deploy/README.md)。

目录布局：

```
/data/nginx-agent/nginx-agent      # 二进制
/data/nginx-agent/config.yaml      # 配置
/data/nginx-agent/backups/         # 本地快照
/data/nginx-agent/tls/             # mTLS 证书（生产）
```

## 配置要点（config.yaml）

```yaml
agent:
  listen: "0.0.0.0:7443"
  tls:
    enabled: false              # 生产务必 true
    cert: /data/nginx-agent/tls/agent.crt
    key:  /data/nginx-agent/tls/agent.key
    ca:   /data/nginx-agent/tls/ca.crt   # 校验中心客户端证书

nginx:
  config_root: /etc/nginx
  binary: /usr/sbin/nginx
  test_cmd: "/usr/sbin/nginx -t"
  reload_cmd: "systemctl reload nginx"    # 或 /usr/sbin/nginx -s reload
  allowed_paths: [/etc/nginx]             # 硬安全边界：只允许读写这些目录
  main_config_name: "nginx.conf"
  allow_main_config: false                # 默认禁止改写主配置（高危）

backup:
  dir: /data/nginx-agent/backups
  retain: 50                              # 本地快照保留份数（改后重启生效）
```

关键说明：

- **`allowed_paths`**：无论逻辑路径如何跳转（含 `../`），最终绝对路径必须落在白名单内。
- **`allow_main_config`**：默认 `false`；主配置改写风险高，仅在确有需要时本地开启。
- **`backup.retain` / `allow_main_config`**：仅本地 `config.yaml` 生效，不支持中心远程修改。

## 配置发现策略

1. 读取主配置 `nginx.conf`（或 `main_config_name` 指定文件）；
2. 递归解析 `include` 指令（glob + 嵌套 include），得到 nginx 实际加载的全部子配置；
3. 兜底扫描 `conf.d/*.conf`、`sites-enabled/*`，防止特殊 include 写法漏检；
4. 从配置内容提取 `server_name`，供中心 UI 按站点呈现。

兼容 OpenResty 等布局：`conf.d` 与 `config_root` 同级时，逻辑路径可能含 `../`，仍受白名单约束。

## 安全闭环（WriteConfig）

```
写入前快照（可选）
    ↓
写入新内容（乐观锁校验 checksum）
    ↓
nginx -t ──失败──► 回滚到快照（新建文件则删除）
    ↓ 通过
reload ──失败──► 回滚 + 再次 reload 尽量恢复服务
    ↓ 通过
返回 ok + new_checksum
```

叠加防护：路径白名单、默认禁改主配置、命令黑名单、原子写入。

## mTLS（生产建议）

1. 用同一 CA 签发中心客户端证书与 Agent 服务端证书；
2. Agent 端 `agent.tls.enabled: true`，配置 cert/key/ca；
3. 中心 `nginx-admin` 端 `agent.tls_enabled: true`，配置对应 cert/key/ca；
4. 防火墙仅允许中心 IP 访问 Agent 的 7443 端口。

## 开发

```bash
make proto    # 重新生成 protobuf（需 protoc + protoc-gen-go + protoc-gen-go-grpc）
make build    # 构建到 bin/nginx-agent
make run      # 本地运行
make vet
./bin/nginx-agent -version
```

> 依赖：grpc v1.67.1、protobuf v1.35.1、yaml.v3。

## 许可证

MIT
