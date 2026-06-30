# nginx-agent 部署（systemd）

每台 nginx 服务器上部署一个 nginx-agent，以 root 运行（需读写 `/etc/nginx`、执行 `systemctl reload nginx`）。

## 文件说明

- `nginx-agent.service` —— systemd unit
- `install.sh` —— 安装/更新脚本（目标机 root 执行）

## 部署步骤

### 1. 在开发机交叉编译（Ubuntu 24 amd64 为例）

```bash
cd nginx-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o nginx-agent ./cmd/nginx-agent
# ARM64 机器用：GOARCH=arm64
```

### 2. 上传到目标机

把 `nginx-agent` 二进制、`config.yaml`、以及 `deploy/` 目录传到目标机同一处，例如：

```bash
scp nginx-agent config.yaml deploy/nginx-agent.service deploy/install.sh \
    root@目标机:/tmp/nginx-agent-pkg/
```

### 3. 目标机上安装

```bash
ssh root@目标机
cd /tmp/nginx-agent-pkg
# 先按本机实际情况改 config.yaml（nginx 路径、reload 命令、监听地址、TLS）
sudo bash install.sh
```

脚本会：创建 `/data/nginx-agent/`（含 `backups/`）、安装二进制与配置、注册 systemd 服务、设开机自启并启动。

## 常用命令

```bash
systemctl status nginx-agent       # 查看状态
systemctl restart nginx-agent      # 重启
journalctl -u nginx-agent -f       # 实时日志
```

## 配置要点（/data/nginx-agent/config.yaml）

```yaml
agent:
  listen: "0.0.0.0:7443"     # gRPC 监听，务必只对内网/中心放行（防火墙）
  tls:
    enabled: false           # 生产务必改 true 开启 mTLS
nginx:
  config_root: /etc/nginx
  reload_cmd: "systemctl reload nginx"   # 按本机实际：或 /usr/sbin/nginx -s reload
  allowed_paths: [/etc/nginx]            # 路径白名单
```

> 升级：重新编译二进制，重复步骤 2-3。`install.sh` 不会覆盖已存在的 `config.yaml`。  
> 若中心使用 nginx-admin **v0.13.0+** 的「删除子配置」，Agent 需 **v0.5.0+**。
