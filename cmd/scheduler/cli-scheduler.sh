#!/bin/bash
# 小红书 AI CLI 任务调度器
# 使用 Gemini CLI 或其他 AI CLI 执行任务

set -e

# 配置
CLI_COMMAND="${AI_CLI:-gemini}"  # 默认使用 gemini，可通过环境变量修改
LOG_FILE="./cli-scheduler.log"

# 评论任务提示词
COMMENT_PROMPT='请帮我在小红书上评论几个帖子：
1. 先搜索"美食"相关的帖子
2. 选择2-3个有趣的帖子
3. 为每个帖子生成真诚自然的评论（不超过50字）
4. 发表评论

使用 xiaohongshu-mcp 的工具完成这个任务。'

# 发帖任务提示词
POST_PROMPT='请帮我在小红书上发布一篇帖子：
1. 主题：生活分享或美食推荐
2. 生成吸引人的标题（不超过20字）
3. 生成有趣的内容（包含3-5个话题标签）
4. 从 ./images 目录选择一张图片
5. 发布帖子

使用 xiaohongshu-mcp 的工具完成这个任务。'

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

run_comment() {
    log "开始执行评论任务..."
    echo "$COMMENT_PROMPT" | $CLI_COMMAND 2>&1 | tee -a "$LOG_FILE"
    log "评论任务完成"
}

run_post() {
    log "开始执行发帖任务..."
    echo "$POST_PROMPT" | $CLI_COMMAND 2>&1 | tee -a "$LOG_FILE"
    log "发帖任务完成"
}

show_help() {
    echo "用法: $0 <command>"
    echo ""
    echo "命令:"
    echo "  comment    执行评论任务"
    echo "  post       执行发帖任务"
    echo "  both       执行评论和发帖"
    echo ""
    echo "环境变量:"
    echo "  AI_CLI     AI CLI 命令 (默认: gemini)"
    echo ""
    echo "示例:"
    echo "  $0 comment"
    echo "  AI_CLI=claude $0 post"
}

case "${1:-}" in
    comment)
        run_comment
        ;;
    post)
        run_post
        ;;
    both)
        run_comment
        sleep 60
        run_post
        ;;
    *)
        show_help
        exit 1
        ;;
esac
