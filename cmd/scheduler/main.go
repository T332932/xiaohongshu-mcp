package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Config 调度器配置
type Config struct {
	MCP struct {
		BaseURL string `yaml:"base_url"` // MCP 服务地址
	} `yaml:"mcp"`
	AI struct {
		BaseURL string `yaml:"base_url"` // AI API 地址 (OpenAI 兼容)
		APIKey  string `yaml:"api_key"`  // API 密钥
		Model   string `yaml:"model"`    // 模型名称
	} `yaml:"ai"`
	Comment struct {
		Enabled       bool     `yaml:"enabled"`        // 是否启用评论
		IntervalMin   int      `yaml:"interval_min"`   // 最小间隔(分钟)
		IntervalMax   int      `yaml:"interval_max"`   // 最大间隔(分钟)
		SearchKeyword string   `yaml:"search_keyword"` // 搜索关键词
		Prompts       []string `yaml:"prompts"`        // AI 评论提示词列表
	} `yaml:"comment"`
	Post struct {
		Enabled     bool     `yaml:"enabled"`      // 是否启用发帖
		IntervalMin int      `yaml:"interval_min"` // 最小间隔(分钟)
		IntervalMax int      `yaml:"interval_max"` // 最大间隔(分钟)
		Topics      []string `yaml:"topics"`       // 发帖主题列表
		ImageDir    string   `yaml:"image_dir"`    // 图片目录
	} `yaml:"post"`
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
			FeedID    string `json:"feed_id"`
			XsecToken string `json:"xsec_token"`
			Title     string `json:"title"`
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

var (
	configPath string
	config     Config
	log        = logrus.New()
)

func init() {
	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径")
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

	log.Info("AI 自动发布调度器启动")

	// 检查登录状态
	if !checkLoginStatus() {
		log.Fatal("未登录，请先登录小红书")
	}

	// 启动调度器
	stopCh := make(chan struct{})
	if config.Comment.Enabled {
		go commentScheduler(stopCh)
	}
	if config.Post.Enabled {
		go postScheduler(stopCh)
	}

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info("正在关闭调度器...")
	close(stopCh)
	time.Sleep(time.Second)
	log.Info("调度器已关闭")
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

// commentScheduler 评论调度器
func commentScheduler(stopCh <-chan struct{}) {
	log.Info("评论调度器已启动")
	for {
		// 随机间隔
		interval := randomInterval(config.Comment.IntervalMin, config.Comment.IntervalMax)
		log.Infof("下次评论将在 %d 分钟后", interval)

		select {
		case <-stopCh:
			return
		case <-time.After(time.Duration(interval) * time.Minute):
			if err := doComment(); err != nil {
				log.Errorf("评论失败: %v", err)
			}
		}
	}
}

// postScheduler 发帖调度器
func postScheduler(stopCh <-chan struct{}) {
	log.Info("发帖调度器已启动")
	for {
		// 随机间隔
		interval := randomInterval(config.Post.IntervalMin, config.Post.IntervalMax)
		log.Infof("下次发帖将在 %d 分钟后", interval)

		select {
		case <-stopCh:
			return
		case <-time.After(time.Duration(interval) * time.Minute):
			if err := doPost(); err != nil {
				log.Errorf("发帖失败: %v", err)
			}
		}
	}
}

// doComment 执行评论任务
func doComment() error {
	log.Info("开始执行评论任务")

	// 搜索帖子
	feeds, err := searchFeeds(config.Comment.SearchKeyword)
	if err != nil {
		return fmt.Errorf("搜索帖子失败: %w", err)
	}
	if len(feeds) == 0 {
		return fmt.Errorf("没有找到帖子")
	}

	// 随机选择一个帖子
	feed := feeds[rand.Intn(len(feeds))]
	log.Infof("选择帖子: %s", feed.Title)

	// 获取帖子详情
	detail, err := getFeedDetail(feed.FeedID, feed.XsecToken)
	if err != nil {
		log.Warnf("获取帖子详情失败，使用标题生成评论: %v", err)
		detail = &FeedDetail{}
		detail.Data.Title = feed.Title
	}

	// 生成评论内容 - 包含完整帖子信息
	prompt := config.Comment.Prompts[rand.Intn(len(config.Comment.Prompts))]
	var commentPrompt string
	if detail.Data.Description != "" {
		commentPrompt = fmt.Sprintf("%s\n\n帖子标题: %s\n帖子内容: %s", prompt, detail.Data.Title, detail.Data.Description)
	} else {
		commentPrompt = fmt.Sprintf("%s\n\n帖子标题: %s", prompt, detail.Data.Title)
	}
	comment, err := generateAIContent(commentPrompt)
	if err != nil {
		return fmt.Errorf("生成评论失败: %w", err)
	}
	log.Infof("生成评论: %s", comment)

	// 发表评论
	if err := postComment(feed.FeedID, feed.XsecToken, comment); err != nil {
		return fmt.Errorf("发表评论失败: %w", err)
	}
	log.Info("评论发表成功")
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
		}{f.FeedID, f.XsecToken, f.Title})
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
		return fmt.Errorf(result.Error)
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
		return fmt.Errorf(result.Error)
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
