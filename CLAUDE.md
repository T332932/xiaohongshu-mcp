# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

小红书 MCP 服务器 - 基于 Go 的 Model Context Protocol 服务，用于自动化与小红书平台的交互。使用 rod 进行无头浏览器自动化。

## 常用命令

```bash
# 运行 MCP 服务 (默认端口 :18060)
go run .

# 非无头模式运行（显示浏览器）
go run . -headless=false

# 运行登录工具
go run cmd/login/main.go

# 运行测试
go test ./...

# 运行单个测试
go test -v ./xiaohongshu -run TestFunctionName

# 格式化代码
gofmt -w .
goimports -w .

# 构建
go build -o xiaohongshu-mcp .
```

## 架构

### 核心组件

- `main.go` - 入口，解析 flags (-headless, -bin, -port)
- `app_server.go` - Gin HTTP 服务器和路由
- `mcp_server.go` - MCP 工具注册
- `mcp_handlers.go` - MCP 工具实现
- `service.go` - 业务逻辑层 (XiaohongshuService)

### xiaohongshu/ 目录

浏览器自动化核心逻辑：
- `login.go` - 登录认证
- `publish.go` - 图文发布
- `publish_video.go` - 视频发布
- `comment_feed.go` - 评论功能
- `feeds.go` - 首页推荐
- `search.go` - 搜索功能
- `like_favorite.go` - 点赞收藏
- `user_profile.go` - 用户信息

### MCP 工具 (12个)

认证: `check_login_status`, `get_login_qrcode`, `delete_cookies`
发布: `publish_content`, `publish_with_video`
浏览: `list_feeds`, `search_feeds`, `get_feed_detail`
交互: `post_comment_to_feed`, `like_feed`, `favorite_feed`, `user_profile`

### HTTP API

MCP 端点: `POST /mcp`
REST API: `/api/v1/*`

## 小红书限制

- 标题: 最多 20 字 (中文=2单位, 英文=1单位)
- 正文: 最多 1000 字
- 标签: 最多 10 个
- 每日发帖: 约 50 篇
- 同一账号只能在一个网页端登录

## Cookie 存储

检查顺序: `/tmp/cookies.json` -> `./cookies.json` -> `COOKIES_PATH` 环境变量

## 开发规范

- 每次修改完后，格式化 Go 源码文件
- 测试中产生的脚本和 build 中间文件，如果没有必要则删除
- 所有 feature 变更需使用分支开发
- 在我未同意之前，不能推送到远程
- 需要: 1.本地 review; 2.远程 PR review
- 不要过度设计，保持代码简洁易读
- 使用中文注释，简洁明了，专业名词可用英文
