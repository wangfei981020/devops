package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"opsplatform-confluence-backend/database"
	"os"
	"strings"
	"sync"
	"time"
)

// ---- 飞书应用 Token 管理 ----

var (
	larkTenantToken  string
	larkTokenExpires time.Time
	larkTokenMu      sync.Mutex
)

func init() {
	ScreenshotBaseURL = os.Getenv("SCREENSHOT_BASE_URL")
}

// getLarkAppCredentials 从数据库读取飞书应用凭证
func getLarkAppCredentials() (appID, appSecret string) {
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'lark_app_id'`).Scan(&appID)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'lark_app_secret'`).Scan(&appSecret)
	return
}

// getLarkTenantToken 获取飞书 tenant_access_token（自动缓存）
func getLarkTenantToken() (string, error) {
	appID, appSecret := getLarkAppCredentials()
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("飞书应用 App ID 或 App Secret 未配置（在系统设置中配置）")
	}

	larkTokenMu.Lock()
	defer larkTokenMu.Unlock()

	// 缓存有效则直接返回
	if larkTenantToken != "" && time.Now().Before(larkTokenExpires) {
		return larkTenantToken, nil
	}

	// 请求新 token
	body := map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	}
	jsonData, _ := json.Marshal(body)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post("https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		"application/json", bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("获取飞书Token失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("解析飞书Token响应失败: %v", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("飞书Token错误 %d: %s", result.Code, result.Msg)
	}

	larkTenantToken = result.TenantAccessToken
	larkTokenExpires = time.Now().Add(time.Duration(result.Expire-60) * time.Second) // 提前60秒过期
	return larkTenantToken, nil
}

// uploadImageToLark 上传图片到飞书，返回 image_key
func uploadImageToLark(imageData []byte) (string, error) {
	token, err := getLarkTenantToken()
	if err != nil {
		return "", err
	}

	// 构建 multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("image_type", "message")
	part, err := writer.CreateFormFile("image", "screenshot.png")
	if err != nil {
		return "", fmt.Errorf("创建form失败: %v", err)
	}
	part.Write(imageData)
	writer.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("POST", "https://open.feishu.cn/open-apis/im/v1/images", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("上传图片失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ImageKey string `json:"image_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("解析上传响应失败: %v", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("上传图片错误 %d: %s", result.Code, result.Msg)
	}

	return result.Data.ImageKey, nil
}

// ---- 基础发送函数 ----

// SendLarkText 发送文本消息到飞书群
func SendLarkText(webhookURL, text string) error {
	body := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": text,
		},
	}
	return sendLarkRequest(webhookURL, body)
}

// SendLarkRichMessage 发送富文本消息到飞书群
func SendLarkRichMessage(webhookURL, title string, contentBlocks [][]map[string]interface{}) error {
	body := map[string]interface{}{
		"msg_type": "post",
		"content": map[string]interface{}{
			"post": map[string]interface{}{
				"zh_cn": map[string]interface{}{
					"title":   title,
					"content": contentBlocks,
				},
			},
		},
	}
	return sendLarkRequest(webhookURL, body)
}

// ---- 截图发送 ----

// ScreenshotBaseURL 截图图片的外部访问地址（备用方案）
var ScreenshotBaseURL string

// SendLarkImageMessage 发送独立图片消息到飞书群（图片全屏显示）
func SendLarkImageMessage(webhookURL, imageKey string) error {
	body := map[string]interface{}{
		"msg_type": "image",
		"content": map[string]string{
			"image_key": imageKey,
		},
	}
	return sendLarkRequest(webhookURL, body)
}

// SendLarkScreenshotResult 发送 Grafana 截图结果到飞书
func SendLarkScreenshotResult(webhookURL, taskName string, dashboards []DashboardScreenshot, imgWidth, imgHeight int) error {
	now := time.Now().Format("2006-01-02 15:04")

	// 判断是否可以用飞书应用 API 上传图片
	appID, appSecret := getLarkAppCredentials()
	canUploadImage := appID != "" && appSecret != ""

	// 先发送文字摘要
	var lines [][]map[string]interface{}
	lines = append(lines, []map[string]interface{}{
		{"tag": "text", "text": fmt.Sprintf("⏰ 时间: %s\n", now)},
	})
	for _, dash := range dashboards {
		if dash.Error != "" {
			lines = append(lines, []map[string]interface{}{
				{"tag": "text", "text": fmt.Sprintf("📋 %s  ❌ %s", dash.Title, dash.Error)},
			})
		} else {
			lines = append(lines, []map[string]interface{}{
				{"tag": "text", "text": fmt.Sprintf("📋 %s  ✅", dash.Title)},
			})
		}
	}
	title := fmt.Sprintf("🖥️ %s - Grafana 监控截图", taskName)
	if err := SendLarkRichMessage(webhookURL, title, lines); err != nil {
		return err
	}

	// 再逐张发送独立图片消息（全屏大图）
	for _, dash := range dashboards {
		if dash.Error != "" {
			continue
		}
		for _, p := range dash.PanelImages {
			if len(p.Data) == 0 {
				continue
			}
			if canUploadImage {
				imageKey, err := uploadImageToLark(p.Data)
				if err != nil {
					log.Printf("上传截图到飞书失败: %v", err)
					continue
				}
				if err := SendLarkImageMessage(webhookURL, imageKey); err != nil {
					log.Printf("发送图片消息失败: %v", err)
				}
			} else if ScreenshotBaseURL != "" {
				key := SaveScreenshotToCache(p.Data)
				imgURL := fmt.Sprintf("%s/api/screenshot-images/%s", ScreenshotBaseURL, key)
				SendLarkText(webhookURL, fmt.Sprintf("📊 %s: %s", p.Title, imgURL))
			}
		}
	}

	return nil
}

// ---- 数据结构 ----

// DashboardScreenshot 截图结果
type DashboardScreenshot struct {
	UID         string
	Title       string
	PanelImages []PanelImage
	Error       string
}

// PanelImage 面板截图
type PanelImage struct {
	PanelID int
	Title   string
	URL     string // 可访问的图片 URL
	Data    []byte // PNG 图片二进制数据
}

// ---- 测试连接 ----

// TestLarkWebhook 测试飞书 Webhook 连接
func TestLarkWebhook(webhookURL string) error {
	return SendLarkText(webhookURL, "🔔 连接测试成功 - 来自 Confluence 运维平台")
}

// HandleTestLarkConnection 测试飞书连接 API
func HandleTestLarkConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.URL == "" {
		respondBadRequest(w, "Webhook URL 不能为空")
		return
	}

	if err := TestLarkWebhook(req.URL); err != nil {
		respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]string{"message": "连接成功，测试消息已发送到飞书群"})
}

// ---- 内部函数 ----

func sendLarkRequest(webhookURL string, body interface{}) error {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("JSON 编码失败: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("飞书返回 %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
		return fmt.Errorf("飞书错误 %d: %s", result.Code, result.Msg)
	}

	return nil
}

// getLarkWebhookURLs 根据连接 IDs 获取飞书 Webhook URLs
func getLarkWebhookURLs(connIDs []int) []string {
	var urls []string
	for _, id := range connIDs {
		_, url, _, _, _, err := GetConnectionByID(id)
		if err == nil && strings.Contains(url, "larksuite.com") || strings.Contains(url, "feishu.cn") || strings.Contains(url, "lark") {
			urls = append(urls, url)
		}
	}
	return urls
}
