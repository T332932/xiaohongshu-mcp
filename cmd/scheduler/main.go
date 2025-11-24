package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*
var templateFS embed.FS

// Config 调度器配置
type Config struct {
	MCP struct {
		BaseURL string `yaml:"base_url" json:"base_url"` // MCP 服务地址
	} `yaml:"mcp" json:"mcp"`
	AI struct {
		Mode    string `yaml:"mode" json:"mode"`         // 模式: "api" 或 "cli"
		BaseURL string `yaml:"base_url" json:"base_url"` // AI API 地址 (OpenAI 兼容)
		APIKey  string `yaml:"api_key" json:"api_key"`   // API 密钥
		Model   string `yaml:"model" json:"model"`       // 模型名称
		// CLI 模式配置
		CLICommand string            `yaml:"cli_command" json:"cli_command"` // CLI 命令: gemini, claude 等
		CLIEnv     map[string]string `yaml:"cli_env" json:"cli_env"`         // CLI 环境变量 (API keys 等)
	} `yaml:"ai" json:"ai"`
	Comment struct {
		Enabled        bool     `yaml:"enabled" json:"enabled"`                 // 是否启用评论
		IntervalMin    int      `yaml:"interval_min" json:"interval_min"`       // 最小间隔(分钟)
		IntervalMax    int      `yaml:"interval_max" json:"interval_max"`       // 最大间隔(分钟)
		SearchKeywords []string `yaml:"search_keywords" json:"search_keywords"` // 搜索关键词列表
		MaxComments    int      `yaml:"max_comments" json:"max_comments"`       // 每次最多评论数
		SystemPrompt   string   `yaml:"system_prompt" json:"system_prompt"`     // AI 系统提示词
	} `yaml:"comment" json:"comment"`
	Post struct {
		Enabled     bool     `yaml:"enabled" json:"enabled"`           // 是否启用发帖
		IntervalMin int      `yaml:"interval_min" json:"interval_min"` // 最小间隔(分钟)
		IntervalMax int      `yaml:"interval_max" json:"interval_max"` // 最大间隔(分钟)
		Topics      []string `yaml:"topics" json:"topics"`             // 发帖主题列表
		ImageDir    string   `yaml:"image_dir" json:"image_dir"`       // 图片目录
	} `yaml:"post" json:"post"`
}

// AIMessage AI 消息
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIRequest AI 请求
type AIRequest struct {
	Model    string      `json:"model"`
	Messages []AIMessage `json:"messages"`
}

// AIResponse AI 响应
type AIResponse struct {
	Choices []struct {
		Message AIMessage `json:"message"`
	} `json:"choices"`
}

// SearchResult 搜索结果
type SearchResult struct {
	Success bool `json:"success"`
	Data    struct {
		Feeds []struct {
			ID        string `json:"id"`
			XsecToken string `json:"xsecToken"`
			NoteCard  struct {
				DisplayTitle string `json:"displayTitle"`
			} `json:"noteCard"`
		} `json:"feeds"`
	} `json:"data"`
}

// FeedDetail 帖子详情
type FeedDetail struct {
	Success bool `json:"success"`
	Data    struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
	} `json:"data"`
}

// CommentDecision AI 评论决策
type CommentDecision struct {
	Comments []struct {
		Index   int    `json:"index"`   // 帖子索引
		Comment string `json:"comment"` // 评论内容
	} `json:"comments"`
}

// PostInfo 帖子信息（用于 AI 选择）
type PostInfo struct {
	Index       int
	FeedID      string
	XsecToken   string
	Title       string
	Description string
}

var (
	configPath      string
	webPort         string
	config          Config
	configMutex     sync.RWMutex
	log             = logrus.New()
	commentHistory  = make(map[string]time.Time) // 已评论的帖子ID -> 评论时间
	historyFilePath = "comment_history.json"

	// 状态跟踪
	lastCommentTime time.Time
	lastPostTime    time.Time
	commentCount    int
	postCount       int
	statusMutex     sync.RWMutex
)

func init() {
	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&webPort, "web", ":8080", "Web 管理界面端口")
}

// loadCommentHistory 加载评论历史
func loadCommentHistory() {
	data, err := os.ReadFile(historyFilePath)
	if err != nil {
		return // 文件不存在则跳过
	}
	var history map[string]string
	if err := json.Unmarshal(data, &history); err != nil {
		return
	}
	for k, v := range history {
		t, _ := time.Parse(time.RFC3339, v)
		commentHistory[k] = t
	}
	log.Infof("加载了 %d 条评论历史", len(commentHistory))
}

// saveCommentHistory 保存评论历史
func saveCommentHistory() {
	history := make(map[string]string)
	// 只保留最近7天的记录
	cutoff := time.Now().AddDate(0, 0, -7)
	for k, v := range commentHistory {
		if v.After(cutoff) {
			history[k] = v.Format(time.RFC3339)
		}
	}
	data, _ := json.MarshalIndent(history, "", "  ")
	os.WriteFile(historyFilePath, data, 0644)
}

// isCommented 检查是否已评论
func isCommented(feedID string) bool {
	_, exists := commentHistory[feedID]
	return exists
}

// markCommented 标记已评论
func markCommented(feedID string) {
	commentHistory[feedID] = time.Now()
	saveCommentHistory()
}

func main() {
	flag.Parse()

	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	log.SetLevel(logrus.InfoLevel)

	// 加载配置
	if err := loadConfig(); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 加载评论历史
	loadCommentHistory()

	log.Info("AI 自动发布调度器启动")

	// 启动 Web 管理界面
	go startWebServer()

	// 检查登录状态
	if !checkLoginStatus() {
		log.Warn("未登录小红书，请通过 Web 界面上传 cookies 或先登录")
	}

	// 启动调度器
	stopCh := make(chan struct{})
	go schedulerManager(stopCh)

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info("正在关闭调度器...")
	close(stopCh)
	time.Sleep(time.Second)
	log.Info("调度器已关闭")
}

// schedulerManager 管理调度器
func schedulerManager(stopCh <-chan struct{}) {
	commentTicker := time.NewTicker(time.Minute)
	postTicker := time.NewTicker(time.Minute)
	defer commentTicker.Stop()
	defer postTicker.Stop()

	var nextCommentTime, nextPostTime time.Time

	for {
		configMutex.RLock()
		commentEnabled := config.Comment.Enabled
		postEnabled := config.Post.Enabled
		commentMin := config.Comment.IntervalMin
		commentMax := config.Comment.IntervalMax
		postMin := config.Post.IntervalMin
		postMax := config.Post.IntervalMax
		configMutex.RUnlock()

		// 初始化下次执行时间
		if nextCommentTime.IsZero() && commentEnabled {
			interval := randomInterval(commentMin, commentMax)
			nextCommentTime = time.Now().Add(time.Duration(interval) * time.Minute)
			log.Infof("下次评论将在 %d 分钟后", interval)
		}
		if nextPostTime.IsZero() && postEnabled {
			interval := randomInterval(postMin, postMax)
			nextPostTime = time.Now().Add(time.Duration(interval) * time.Minute)
			log.Infof("下次发帖将在 %d 分钟后", interval)
		}

		select {
		case <-stopCh:
			return
		case <-commentTicker.C:
			if commentEnabled && time.Now().After(nextCommentTime) {
				if err := doComment(); err != nil {
					log.Errorf("评论失败: %v", err)
				} else {
					statusMutex.Lock()
					lastCommentTime = time.Now()
					commentCount++
					statusMutex.Unlock()
				}
				interval := randomInterval(commentMin, commentMax)
				nextCommentTime = time.Now().Add(time.Duration(interval) * time.Minute)
				log.Infof("下次评论将在 %d 分钟后", interval)
			}
		case <-postTicker.C:
			if postEnabled && time.Now().After(nextPostTime) {
				if err := doPost(); err != nil {
					log.Errorf("发帖失败: %v", err)
				} else {
					statusMutex.Lock()
					lastPostTime = time.Now()
					postCount++
					statusMutex.Unlock()
				}
				interval := randomInterval(postMin, postMax)
				nextPostTime = time.Now().Add(time.Duration(interval) * time.Minute)
				log.Infof("下次发帖将在 %d 分钟后", interval)
			}
		}
	}
}

// loadConfig 加载配置文件
func loadConfig() error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	// 替换环境变量
	content := os.ExpandEnv(string(data))
	return yaml.Unmarshal([]byte(content), &config)
}

// checkLoginStatus 检查登录状态
func checkLoginStatus() bool {
	resp, err := http.Get(config.MCP.BaseURL + "/api/v1/login/status")
	if err != nil {
		log.Errorf("检查登录状态失败: %v", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			LoggedIn bool `json:"logged_in"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	return result.Success && result.Data.LoggedIn
}

// doComment 执行评论任务
func doComment() error {
	log.Info("开始执行评论任务")

	// 搜索多个关键词的帖子
	var allPosts []PostInfo
	skippedCount := 0
	for _, keyword := range config.Comment.SearchKeywords {
		feeds, err := searchFeeds(keyword)
		if err != nil {
			log.Warnf("搜索关键词 %s 失败: %v", keyword, err)
			continue
		}
		for _, f := range feeds {
			// 跳过已评论的帖子
			if isCommented(f.FeedID) {
				skippedCount++
				continue
			}
			// 获取帖子详情
			detail, err := getFeedDetail(f.FeedID, f.XsecToken)
			desc := ""
			title := f.Title
			if err == nil {
				desc = detail.Data.Description
				title = detail.Data.Title
			}
			allPosts = append(allPosts, PostInfo{
				Index:       len(allPosts),
				FeedID:      f.FeedID,
				XsecToken:   f.XsecToken,
				Title:       title,
				Description: desc,
			})
		}
	}

	if len(allPosts) == 0 {
		if skippedCount > 0 {
			log.Infof("所有帖子都已评论过(共 %d 个)，本轮跳过", skippedCount)
			return nil // 不是错误，正常跳过
		}
		return fmt.Errorf("没有找到任何帖子")
	}
	log.Infof("共找到 %d 个新帖子(跳过 %d 个已评论)", len(allPosts), skippedCount)

	// 构建帖子列表给 AI
	var postList strings.Builder
	for _, p := range allPosts {
		postList.WriteString(fmt.Sprintf("\n[%d] 标题: %s\n", p.Index, p.Title))
		if p.Description != "" {
			// 截断过长的内容
			desc := p.Description
			if len([]rune(desc)) > 200 {
				desc = string([]rune(desc)[:200]) + "..."
			}
			postList.WriteString(fmt.Sprintf("内容: %s\n", desc))
		}
	}

	// 让 AI 选择并生成评论
	maxComments := config.Comment.MaxComments
	if maxComments <= 0 {
		maxComments = 3
	}

	prompt := fmt.Sprintf(`%s

以下是可选的帖子列表:
%s

请选择最值得评论的帖子(最多%d个)，为每个生成一条评论。
评论要求：真诚自然，不超过50字，不要使用表情符号。

请严格按以下 JSON 格式返回：
{"comments":[{"index":0,"comment":"评论内容"},{"index":2,"comment":"评论内容"}]}

只返回 JSON，不要其他内容。`, config.Comment.SystemPrompt, postList.String(), maxComments)

	response, err := generateAIContent(prompt)
	if err != nil {
		return fmt.Errorf("AI 生成评论失败: %w", err)
	}

	// 解析 AI 返回的 JSON
	// 清理可能的 markdown 代码块
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var decision CommentDecision
	if err := json.Unmarshal([]byte(response), &decision); err != nil {
		return fmt.Errorf("解析 AI 响应失败: %w, 响应: %s", err, response)
	}

	if len(decision.Comments) == 0 {
		log.Info("AI 没有选择任何帖子评论")
		return nil
	}

	// 发表评论
	successCount := 0
	for _, c := range decision.Comments {
		if c.Index < 0 || c.Index >= len(allPosts) {
			log.Warnf("无效的帖子索引: %d", c.Index)
			continue
		}
		post := allPosts[c.Index]
		log.Infof("评论帖子 [%s]: %s", post.Title, c.Comment)

		if err := postComment(post.FeedID, post.XsecToken, c.Comment); err != nil {
			log.Errorf("发表评论失败: %v", err)
			continue
		}
		successCount++
		markCommented(post.FeedID) // 标记已评论

		// 评论间隔，避免太快
		time.Sleep(time.Duration(5+rand.Intn(10)) * time.Second)
	}

	log.Infof("成功发表 %d 条评论", successCount)
	return nil
}

// doPost 执行发帖任务
func doPost() error {
	log.Info("开始执行发帖任务")

	// 随机选择主题
	topic := config.Post.Topics[rand.Intn(len(config.Post.Topics))]
	log.Infof("选择主题: %s", topic)

	// 生成标题和内容
	titlePrompt := fmt.Sprintf("为小红书帖子生成一个吸引人的标题，主题是: %s\n要求: 不超过20个字，不要使用引号", topic)
	title, err := generateAIContent(titlePrompt)
	if err != nil {
		return fmt.Errorf("生成标题失败: %w", err)
	}
	// 清理标题
	title = strings.TrimSpace(title)
	title = strings.Trim(title, "\"'")
	if len([]rune(title)) > 20 {
		title = string([]rune(title)[:20])
	}
	log.Infof("生成标题: %s", title)

	contentPrompt := fmt.Sprintf("为小红书帖子生成内容，主题是: %s，标题是: %s\n要求: 不超过500字，包含3-5个相关话题标签(用#开头)", topic, title)
	content, err := generateAIContent(contentPrompt)
	if err != nil {
		return fmt.Errorf("生成内容失败: %w", err)
	}
	log.Infof("生成内容: %s", content)

	// 获取图片
	images, err := getRandomImages(config.Post.ImageDir, 1)
	if err != nil {
		return fmt.Errorf("获取图片失败: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("图片目录为空")
	}

	// 发布帖子
	if err := publishContent(title, content, images); err != nil {
		return fmt.Errorf("发布帖子失败: %w", err)
	}
	log.Info("帖子发布成功")
	return nil
}

// searchFeeds 搜索帖子
func searchFeeds(keyword string) ([]struct {
	FeedID    string
	XsecToken string
	Title     string
}, error) {
	reqBody, _ := json.Marshal(map[string]string{"keyword": keyword})
	resp, err := http.Post(config.MCP.BaseURL+"/api/v1/feeds/search", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var feeds []struct {
		FeedID    string
		XsecToken string
		Title     string
	}
	for _, f := range result.Data.Feeds {
		feeds = append(feeds, struct {
			FeedID    string
			XsecToken string
			Title     string
		}{f.ID, f.XsecToken, f.NoteCard.DisplayTitle})
	}
	return feeds, nil
}

// getFeedDetail 获取帖子详情
func getFeedDetail(feedID, xsecToken string) (*FeedDetail, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"feed_id":    feedID,
		"xsec_token": xsecToken,
	})
	resp, err := http.Post(config.MCP.BaseURL+"/api/v1/feeds/detail", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result FeedDetail
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("获取帖子详情失败")
	}
	return &result, nil
}

// generateAIContent 调用 AI 生成内容
func generateAIContent(prompt string) (string, error) {
	configMutex.RLock()
	mode := config.AI.Mode
	cliCommand := config.AI.CLICommand
	configMutex.RUnlock()

	// CLI 模式
	if mode == "cli" {
		return generateAIContentCLI(cliCommand, prompt)
	}

	// API 模式 (默认)
	req := AIRequest{
		Model: config.AI.Model,
		Messages: []AIMessage{
			{Role: "user", Content: prompt},
		},
	}
	reqBody, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", config.AI.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.AI.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("AI API 错误: %s", string(body))
	}

	var result AIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("AI 没有返回内容")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// generateAIContentCLI 通过 CLI 命令生成内容
func generateAIContentCLI(cliCommand, prompt string) (string, error) {
	if cliCommand == "" {
		cliCommand = "gemini" // 默认使用 gemini
	}

	log.Debugf("执行 CLI 命令: %s", cliCommand)

	// 创建命令，通过 stdin 传入 prompt
	cmd := exec.Command(cliCommand)
	cmd.Stdin = strings.NewReader(prompt)

	// 设置环境变量
	configMutex.RLock()
	cliEnv := config.AI.CLIEnv
	configMutex.RUnlock()

	if len(cliEnv) > 0 {
		// 继承当前环境并添加 CLI 环境变量
		cmd.Env = os.Environ()
		for k, v := range cliEnv {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// 执行并获取输出
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("CLI 执行失败: %s, stderr: %s", err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("CLI 执行失败: %w", err)
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return "", fmt.Errorf("CLI 没有返回内容")
	}

	return result, nil
}

// postComment 发表评论
func postComment(feedID, xsecToken, content string) error {
	reqBody, _ := json.Marshal(map[string]string{
		"feed_id":    feedID,
		"xsec_token": xsecToken,
		"content":    content,
	})
	resp, err := http.Post(config.MCP.BaseURL+"/api/v1/feeds/comment", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("评论失败: %s", result.Error)
	}
	return nil
}

// publishContent 发布帖子
func publishContent(title, content string, images []string) error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"title":   title,
		"content": content,
		"images":  images,
	})
	resp, err := http.Post(config.MCP.BaseURL+"/api/v1/publish", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("发布失败: %s", result.Error)
	}
	return nil
}

// getRandomImages 从目录随机获取图片
func getRandomImages(dir string, count int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var images []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") ||
			strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".gif") {
			images = append(images, dir+"/"+e.Name())
		}
	}

	if len(images) == 0 {
		return nil, nil
	}

	// 随机打乱
	rand.Shuffle(len(images), func(i, j int) {
		images[i], images[j] = images[j], images[i]
	})

	if count > len(images) {
		count = len(images)
	}
	return images[:count], nil
}

// randomInterval 生成随机间隔
func randomInterval(min, max int) int {
	if min >= max {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// ========== Web 管理界面 ==========

// startWebServer 启动 Web 管理服务器
func startWebServer() {
	mux := http.NewServeMux()

	// 页面
	mux.HandleFunc("/", handleIndex)

	// API
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/upload/cookies", handleUploadCookies)
	mux.HandleFunc("/api/toggle/comment", handleToggleComment)
	mux.HandleFunc("/api/toggle/post", handleTogglePost)
	mux.HandleFunc("/api/run/comment", handleRunComment)
	mux.HandleFunc("/api/run/post", handleRunPost)
	mux.HandleFunc("/api/check-login", handleCheckLogin)
	mux.HandleFunc("/api/save/cookie-text", handleSaveCookieText)

	log.Infof("Web 管理界面启动在 http://localhost%s", webPort)
	if err := http.ListenAndServe(webPort, mux); err != nil {
		log.Errorf("Web 服务器错误: %v", err)
	}
}

// handleIndex 主页
func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	configMutex.RLock()
	data := struct {
		Config Config
	}{
		Config: config,
	}
	configMutex.RUnlock()

	tmpl.Execute(w, data)
}

// handleStatus 获取状态
func handleStatus(w http.ResponseWriter, r *http.Request) {
	configMutex.RLock()
	commentEnabled := config.Comment.Enabled
	postEnabled := config.Post.Enabled
	configMutex.RUnlock()

	statusMutex.RLock()
	status := map[string]interface{}{
		"comment_enabled":   commentEnabled,
		"post_enabled":      postEnabled,
		"last_comment_time": lastCommentTime.Format(time.RFC3339),
		"last_post_time":    lastPostTime.Format(time.RFC3339),
		"comment_count":     commentCount,
		"post_count":        postCount,
		"login_status":      checkLoginStatus(),
	}
	statusMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleConfig 获取/更新配置
func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		configMutex.RLock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
		configMutex.RUnlock()
		return
	}

	if r.Method == "POST" {
		var newConfig Config
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		configMutex.Lock()
		config = newConfig
		configMutex.Unlock()

		// 保存到文件
		if err := saveConfig(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// saveConfig 保存配置到文件
func saveConfig() error {
	configMutex.RLock()
	data, err := yaml.Marshal(config)
	configMutex.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// handleUploadCookies 上传 cookies
func handleUploadCookies(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析 multipart form
	err := r.ParseMultipartForm(10 << 20) // 10MB max
	if err != nil {
		http.Error(w, "解析表单失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("cookies")
	if err != nil {
		http.Error(w, "获取文件失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 读取内容
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "读取文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 验证 JSON 格式
	var cookies interface{}
	if err := json.Unmarshal(content, &cookies); err != nil {
		http.Error(w, "无效的 JSON 格式: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 保存到 /tmp/cookies.json
	cookiePath := "/tmp/cookies.json"
	if err := os.WriteFile(cookiePath, content, 0644); err != nil {
		http.Error(w, "保存文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("Cookies 已上传到 %s", cookiePath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"path":    cookiePath,
	})
}

// handleToggleComment 切换评论开关
func handleToggleComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configMutex.Lock()
	config.Comment.Enabled = !config.Comment.Enabled
	enabled := config.Comment.Enabled
	configMutex.Unlock()

	saveConfig()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

// handleTogglePost 切换发帖开关
func handleTogglePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configMutex.Lock()
	config.Post.Enabled = !config.Post.Enabled
	enabled := config.Post.Enabled
	configMutex.Unlock()

	saveConfig()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

// handleRunComment 手动执行评论
func handleRunComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		if err := doComment(); err != nil {
			log.Errorf("手动评论失败: %v", err)
		} else {
			statusMutex.Lock()
			lastCommentTime = time.Now()
			commentCount++
			statusMutex.Unlock()
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

// handleRunPost 手动执行发帖
func handleRunPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		if err := doPost(); err != nil {
			log.Errorf("手动发帖失败: %v", err)
		} else {
			statusMutex.Lock()
			lastPostTime = time.Now()
			postCount++
			statusMutex.Unlock()
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

// handleCheckLogin 检查登录状态
func handleCheckLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	loggedIn := checkLoginStatus()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logged_in": loggedIn,
		"message": func() string {
			if loggedIn {
				return "登录成功"
			}
			return "未登录，请检查 Cookie 格式是否正确"
		}(),
	})
}

// handleSaveCookieText 保存粘贴的 Cookie 文本
func handleSaveCookieText(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CookieText string `json:"cookie_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.CookieText == "" {
		http.Error(w, "Cookie 文本不能为空", http.StatusBadRequest)
		return
	}

	// 解析 cookie 字符串并转换为 JSON 数组
	cookies := parseCookieString(req.CookieText)
	if len(cookies) == 0 {
		http.Error(w, "无法解析 Cookie 字符串", http.StatusBadRequest)
		return
	}

	// 转换为 JSON
	data, err := json.MarshalIndent(cookies, "", "  ")
	if err != nil {
		http.Error(w, "转换 JSON 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 保存到文件
	cookiePath := "/tmp/cookies.json"
	if err := os.WriteFile(cookiePath, data, 0644); err != nil {
		http.Error(w, "保存文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("Cookie 文本已解析并保存到 %s (共 %d 个)", cookiePath, len(cookies))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"path":    cookiePath,
		"count":   len(cookies),
	})
}

// parseCookieString 解析 cookie 字符串为 JSON 数组
func parseCookieString(cookieStr string) []map[string]interface{} {
	var cookies []map[string]interface{}

	// 按 ; 分割
	pairs := strings.Split(cookieStr, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		// 按 = 分割 name 和 value
		idx := strings.Index(pair, "=")
		if idx == -1 {
			continue
		}

		name := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])

		if name == "" {
			continue
		}

		// 创建 cookie 对象 (Rod 浏览器格式)
		cookie := map[string]interface{}{
			"name":   name,
			"value":  value,
			"domain": ".xiaohongshu.com",
			"path":   "/",
		}
		cookies = append(cookies, cookie)
	}

	return cookies
}
