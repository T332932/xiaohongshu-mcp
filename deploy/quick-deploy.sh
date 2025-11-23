#!/bin/bash
# 小红书调度器一键部署脚本

set -e

# 配置
INSTALL_DIR="/opt/xiaohongshu"
WEB_PORT="8081"
MCP_PORT="18060"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=========================================="
echo "  小红书调度器一键部署"
echo "=========================================="

# 先构建（不需要 root）
echo "[1/6] 构建程序..."
cd "$PROJECT_DIR"
go build -o /tmp/xiaohongshu-mcp .
go build -o /tmp/scheduler ./cmd/scheduler
echo "  构建完成"

# 检查 root 权限
if [ "$EUID" -ne 0 ]; then
    echo ""
    echo "构建完成，现在需要 root 权限安装"
    echo "请运行: sudo $0"
    exit 0
fi

# 创建目录
echo "[2/6] 创建安装目录..."
mkdir -p $INSTALL_DIR
mkdir -p $INSTALL_DIR/images

# 复制二进制文件
echo "[3/6] 安装程序..."
cp /tmp/xiaohongshu-mcp $INSTALL_DIR/
cp /tmp/scheduler $INSTALL_DIR/
chmod +x $INSTALL_DIR/xiaohongshu-mcp $INSTALL_DIR/scheduler

# 复制配置
echo "[4/6] 复制配置文件..."
if [ ! -f $INSTALL_DIR/config.yaml ]; then
    cp "$PROJECT_DIR/cmd/scheduler/config.example.yaml" $INSTALL_DIR/config.yaml
    echo "  已创建默认配置: $INSTALL_DIR/config.yaml"
    echo "  请编辑配置文件设置 CLI 命令等参数"
fi

# 创建 MCP 服务
echo "[5/6] 创建 systemd 服务..."
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
echo "[6/6] 启动服务..."
systemctl daemon-reload
systemctl enable xiaohongshu-mcp xiaohongshu-scheduler
systemctl start xiaohongshu-mcp
sleep 2
systemctl start xiaohongshu-scheduler

# 完成
echo ""
echo "部署完成!"
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
