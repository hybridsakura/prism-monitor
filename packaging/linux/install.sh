#!/usr/bin/env bash
# Prism Monitor Linux 一键安装脚本
# 用法：curl -fsSL https://raw.githubusercontent.com/hybridsakura/prism-monitor/main/packaging/linux/install.sh | sudo bash
set -euo pipefail

REPO="hybridsakura/prism-monitor"
INSTALL_DIR="/opt/prism-monitor"
BIN_PATH="/usr/local/bin/prism-monitor"
SERVICE_PATH="/etc/systemd/system/prism-monitor.service"
DATA_DIR="/var/lib/prism-monitor"

echo "==> 正在检测系统架构..."
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)   GOARCH="amd64" ;;
    aarch64)  GOARCH="arm64" ;;
    armv7l)   GOARCH="armv7" ;;
    *) echo "不支持的架构: $ARCH" && exit 1 ;;
esac

LATEST_TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
if [ -z "$LATEST_TAG" ]; then
    echo "获取最新版本失败，回退到 v0.1.0"
    LATEST_TAG="v0.1.0"
fi
TARBALL="prism-monitor-linux-${GOARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${TARBALL}"

echo "==> 下载版本 ${LATEST_TAG} (${GOARCH})..."
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT
curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${TARBALL}"

mkdir -p "$INSTALL_DIR" "$DATA_DIR"
tar -xzf "${TMP_DIR}/${TARBALL}" -C "$INSTALL_DIR"
chmod +x "${INSTALL_DIR}/prism-monitor"
ln -sf "${INSTALL_DIR}/prism-monitor" "$BIN_PATH"

echo "==> 写入 systemd 服务..."
cat <<EOF > "$SERVICE_PATH"
[Unit]
Description=Prism Monitor - Server Resource Monitoring WebUI
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${DATA_DIR}
ExecStart=${BIN_PATH}
Restart=always
RestartSec=3
Environment=PRISM_PORT=21120
Environment=PRISM_DB=${DATA_DIR}/prism-monitor.db
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now prism-monitor

echo "==> Prism Monitor 安装成功！"
echo "    Web 访问: http://<服务器IP>:21120"
echo "    服务状态: systemctl status prism-monitor"
echo "    查看日志: journalctl -u prism-monitor -f"
