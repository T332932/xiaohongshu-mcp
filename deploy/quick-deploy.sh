#!/bin/bash
# 小红书调度器一键部署脚本

set -e

# 配置
INSTALL_DIR="/opt/xiaohongshu"
WEB_PORT="8081"
MCP_PORT="18060"
USER=$(whoami)

echo "=========================================="
echo "  小红书调度器一键部署"
echo "=========================================="

# 检查 root 权限
if [ "$EUID" -ne 0 ]; then
    echo "请使用 sudo 运行此脚本"
    exit 1
fi

# 创建目录
echo "[1/6] 创建安装目录..."
mkdir -p $INSTALL_DIR
mkdir -p $INSTALL_DIR/images

# 构建
echo "[2/6] 构建程序..."
cd "$(dirname "$0")/.."
go build -o $INSTALL_DIR/xiaohongshu-mcp .
go build -o $INSTALL_DIR/scheduler ./cmd/scheduler

# 复制配置
echo "[3/6] 复制配置文件..."
if [ ! -f $INSTALL_DIR/config.yaml ]; then
    cp cmd/scheduler/config.example.yaml $INSTALL_DIR/config.yaml
    echo "  已创建默认配置: $INSTALL_DIR/config.yaml"
    echo "  请编辑配置文件设置 CLI 命令等参数"
fi

# 创建 MCP 服务
echo "[4/6] 创建 systemd 服务..."
cat > /etc/systemd/system/xiaohongshu-mcp.service << EOF
[Unit]
Description=Xiaohongshu MCP Server
After=network.target

[Service]
Type=simple
User=$SUDO_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/xiaohongshu-mcp -port :$MCP_PORT -headless=true
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 创建调度器服务
cat > /etc/systemd/system/xiaohongshu-scheduler.service << EOF
[Unit]
Description=Xiaohongshu AI Scheduler
After=network.target xiaohongshu-mcp.service
Requires=xiaohongshu-mcp.service

[Service]
Type=simple
User=$SUDO_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/scheduler -config $INSTALL_DIR/config.yaml -web :$WEB_PORT
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 重载 systemd
echo "[5/6] 启动服务..."
systemctl daemon-reload
systemctl enable xiaohongshu-mcp xiaohongshu-scheduler
systemctl start xiaohongshu-mcp
sleep 2
systemctl start xiaohongshu-scheduler

# 完成
echo "[6/6] 部署完成!"
echo ""
echo "=========================================="
echo "  部署信息"
echo "=========================================="
echo ""
echo "  Web 管理界面: http://localhost:$WEB_PORT"
echo "  MCP 服务地址: http://localhost:$MCP_PORT"
echo ""
echo "  配置文件: $INSTALL_DIR/config.yaml"
echo "  图片目录: $INSTALL_DIR/images"
echo ""
echo "  常用命令:"
echo "    查看状态: systemctl status xiaohongshu-scheduler"
echo "    查看日志: journalctl -u xiaohongshu-scheduler -f"
echo "    重启服务: systemctl restart xiaohongshu-scheduler"
echo "    编辑配置: vim $INSTALL_DIR/config.yaml"
echo ""
echo "  注意: 请先上传 cookies.json 到 /tmp/ 或 $INSTALL_DIR/"
echo "=========================================="
