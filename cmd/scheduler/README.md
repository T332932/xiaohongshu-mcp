# AI 自动发布调度器

使用 AI 自动生成内容并定时发布评论和帖子到小红书。

## 功能

- **自动评论**: 定时搜索帖子并使用 AI 生成评论
- **自动发帖**: 定时使用 AI 生成标题和内容发布帖子
- **随机间隔**: 避免被平台检测到机器行为
- **多主题支持**: 配置多个评论提示词和发帖主题

## 快速开始

### 1. 配置

复制示例配置并编辑：

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，设置：
- MCP 服务地址
- AI API 密钥和模型
- 评论/发帖间隔和主题

### 2. 设置环境变量

```bash
export OPENAI_API_KEY="your-api-key"
```

### 3. 运行

确保 MCP 服务已启动：

```bash
# 在项目根目录
go run .
```

启动调度器：

```bash
go run ./cmd/scheduler -config=config.yaml
```

## 配置说明

### AI 配置

支持 OpenAI 兼容接口，如：
- OpenAI: `https://api.openai.com/v1`
- Azure OpenAI: `https://your-resource.openai.azure.com/openai/deployments/your-deployment`
- 国内代理: 各种兼容接口

### 评论配置

- `search_keyword`: 搜索关键词，找相关帖子评论
- `prompts`: AI 评论提示词，随机选择生成不同风格

### 发帖配置

- `topics`: 发帖主题列表，AI 根据主题生成内容
- `image_dir`: 图片目录，发帖时随机选择图片

## 生产部署

使用 systemd 部署：

```bash
cd deploy
sudo ./deploy.sh
```

详见 `deploy/` 目录。

## 注意事项

1. 首次使用需要先登录小红书
2. 设置合理的间隔避免被平台限制
3. 小红书每日发帖上限约 50 篇
4. 评论内容要真实自然，避免违规
