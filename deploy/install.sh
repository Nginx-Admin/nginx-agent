#!/usr/bin/env bash
# nginx-agent 安装/更新脚本（在【每台 nginx 服务器】上以 root 执行）
# 用法：
#   1. 把构建好的二进制 nginx-agent 和 config.yaml 放到本脚本同目录
#   2. sudo bash install.sh
set -euo pipefail

INSTALL_DIR=/data/nginx-agent
SERVICE=nginx-agent
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "请用 root 运行：sudo bash install.sh" >&2
  exit 1
fi

echo "==> 创建目录 $INSTALL_DIR 及 backups/"
mkdir -p "$INSTALL_DIR/backups"

BIN_SRC=""
for c in "$HERE/nginx-agent" "$HERE/../nginx-agent"; do
  if [[ -f "$c" ]]; then BIN_SRC="$c"; break; fi
done
if [[ -z "$BIN_SRC" ]]; then
  echo "未找到 nginx-agent 二进制，请先构建并放到本脚本同目录。" >&2
  echo "构建：CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o nginx-agent ./cmd/nginx-agent" >&2
  exit 1
fi
echo "==> 安装二进制：$BIN_SRC -> $INSTALL_DIR/nginx-agent"
install -m 0755 "$BIN_SRC" "$INSTALL_DIR/nginx-agent"

# 配置：存在则不覆盖（每台机器的路径/命令可能不同）
if [[ -f "$INSTALL_DIR/config.yaml" ]]; then
  echo "==> 配置已存在，保留不覆盖：$INSTALL_DIR/config.yaml"
elif [[ -f "$HERE/config.yaml" ]]; then
  echo "==> 安装配置：$INSTALL_DIR/config.yaml（请按本机实际情况修改 nginx 路径/reload 命令）"
  install -m 0640 "$HERE/config.yaml" "$INSTALL_DIR/config.yaml"
elif [[ -f "$HERE/../config.yaml" ]]; then
  install -m 0640 "$HERE/../config.yaml" "$INSTALL_DIR/config.yaml"
else
  echo "!! 未找到 config.yaml，请手动放到 $INSTALL_DIR/config.yaml 后再启动" >&2
fi

echo "==> 安装 systemd unit"
install -m 0644 "$HERE/nginx-agent.service" /etc/systemd/system/nginx-agent.service

echo "==> 重载 systemd 并启用开机自启"
systemctl daemon-reload
systemctl enable "$SERVICE"
systemctl restart "$SERVICE"

echo "==> 完成。状态："
systemctl --no-pager status "$SERVICE" || true
echo
echo "查看日志： journalctl -u $SERVICE -f"
