#!/bin/bash
# 小红书 MCP 服务部署脚本

set -e

# 配置
APP_NAME="xiaohongshu-mcp"
SCHEDULER_NAME="xiaohongshu-scheduler"
INSTALL_DIR="/opt/xiaohongshu-mcp"
SERVICE_USER="xiaohongshu"

echo "=== 小红书 MCP 服务部署脚本 ==="

# 检查 root 权限
if [ "$EUID" -ne 0 ]; then
    echo "请使用 root 权限运行此脚本"
    exit 1
fi

# 创建用户
if ! id "$SERVICE_USER" &>/dev/null; then
    echo "创建用户 $SERVICE_USER..."
    useradd -r -s /bin/false $SERVICE_USER
fi

# 创建安装目录
echo "创建安装目录..."
mkdir -p $INSTALL_DIR/{bin,images,data}
chown -R $SERVICE_USER:$SERVICE_USER $INSTALL_DIR

# 编译程序
echo "编译 MCP 服务..."
cd "$(dirname "$0")/.."
CGO_ENABLED=0 go build -o $INSTALL_DIR/bin/$APP_NAME .

echo "编译调度器..."
CGO_ENABLED=0 go build -o $INSTALL_DIR/bin/$SCHEDULER_NAME ./cmd/scheduler

# 复制配置文件
echo "复制配置文件..."
if [ ! -f "$INSTALL_DIR/scheduler-config.yaml" ]; then
    cp cmd/scheduler/config.example.yaml $INSTALL_DIR/scheduler-config.yaml
    echo "请编辑 $INSTALL_DIR/scheduler-config.yaml 配置调度器"
fi

# 设置权限
chmod +x $INSTALL_DIR/bin/*
chown -R $SERVICE_USER:$SERVICE_USER $INSTALL_DIR

# 安装 systemd 服务
echo "安装 systemd 服务..."
cp deploy/xiaohongshu-mcp.service /etc/systemd/system/
cp deploy/xiaohongshu-scheduler.service /etc/systemd/system/

# 重新加载 systemd
systemctl daemon-reload

echo ""
echo "=== 部署完成 ==="
echo ""
echo "使用说明:"
echo "1. 编辑配置文件: $INSTALL_DIR/scheduler-config.yaml"
echo "2. 设置环境变量 OPENAI_API_KEY"
echo ""
echo "启动 MCP 服务:"
echo "  systemctl start xiaohongshu-mcp"
echo "  systemctl enable xiaohongshu-mcp"
echo ""
echo "启动调度器:"
echo "  systemctl start xiaohongshu-scheduler"
echo "  systemctl enable xiaohongshu-scheduler"
echo ""
echo "查看日志:"
echo "  journalctl -u xiaohongshu-mcp -f"
echo "  journalctl -u xiaohongshu-scheduler -f"
echo ""
echo "注意: 首次使用需要先登录小红书"
echo "  $INSTALL_DIR/bin/$APP_NAME -headless=false"
