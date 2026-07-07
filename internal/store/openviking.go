// OpenViking HTTP 客户端：封装 OpenViking 上下文数据库的 REST API。
//
// OpenViking 是字节开源的 AI Agent 上下文数据库，采用文件系统范式管理记忆/资源/技能。
// 支持本地部署（默认端口 1933）和火山引擎云上版本。
//
// REST API 文档: https://docs.openviking.ai/zh/api/01-overview
//
// 鉴权方式:
//   - Bearer Token: Authorization: Bearer <key>（推荐）
//   - API Key: X-API-Key: <key>（备选）
//
// 本客户端支持降级：连接失败时自动禁用，仅使用本地 JSON 存储，不影响主流程。
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OpenVikingConfig 是 OpenViking 客户端配置。
type OpenVikingConfig struct {
	BaseURL  string // OpenViking 服务地址（本地: http://localhost:1933，云上: 火山引擎endpoint）
	Enabled  bool   // 是否启用
	APIKey   string // API Key（鉴权用，云上版本必填）
	Account  string // 多租户账号（可选，trusted 模式）
	User     string // 多租户用户（可选，trusted 模式）
	Timeout  int    // HTTP 超时（秒）
}

// OpenVikingClient 封装 OpenViking HTTP API 交互。
type OpenVikingClient struct {
	baseURL    string
	apiKey     string
	account    string
	user       string
	httpClient *http.Client
	mu         sync.RWMutex
	enabled    bool // 运行时状态（连接失败时降级为 false）
}

// NewOpenVikingClient 创建 OpenViking 客户端。
func NewOpenVikingClient(cfg *OpenVikingConfig) *OpenVikingClient {
	if cfg == nil {
		return &OpenVikingClient{enabled: false}
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	client := &OpenVikingClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		account: cfg.Account,
		user:    cfg.User,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		enabled: cfg.Enabled,
	}

	// 启用时检测连接
	if client.enabled {
		if err := client.ping(context.Background()); err != nil {
			log.Printf("[OpenViking] 连接失败，降级为仅本地存储: %v", err)
			client.enabled = false
		} else {
			log.Printf("[OpenViking] 连接成功: %s", client.baseURL)
		}
	}

	return client
}

// IsEnabled 返回 OpenViking 是否可用。
func (c *OpenVikingClient) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// --- 文件系统操作 ---

// toVikingURI 将简单路径转换为 viking:// URI。
// 如 "scripts/the_dark_house/background" → "viking://resources/scripts/the_dark_house/background"
func toVikingURI(path string) string {
	path = strings.TrimPrefix(path, "/")
	if strings.HasPrefix(path, "viking://") {
		return path
	}
	return "viking://resources/" + path
}

// WriteContext 写入上下文资源（content/write API）。
// path 格式如 "scripts/the_dark_house/background"
// 使用 POST /api/v1/content/write 接口。
// 新文件使用 mode=create（自动创建父目录），已存在的文件使用 mode=replace。
func (c *OpenVikingClient) WriteContext(ctx context.Context, path string, content string) error {
	if !c.IsEnabled() {
		return nil // 降级模式，静默跳过
	}

	uri := toVikingURI(path)
	// create 模式要求有扩展名，无扩展名追加 .md
	if filepath.Ext(path) == "" {
		uri += ".md"
	}

	// 先尝试 create 模式（创建新文件），失败则用 replace（覆盖已有文件）
	body := map[string]interface{}{
		"uri":     uri,
		"content": content,
		"mode":    "create",
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/content/write", body)
	if err != nil {
		c.markDisabled()
		return fmt.Errorf("写入上下文失败: %w", err)
	}
	defer resp.Body.Close()

	// 409 Conflict 表示文件已存在，改用 replace 模式
	if resp.StatusCode == http.StatusConflict {
		body["mode"] = "replace"
		resp2, err2 := c.doRequest(ctx, "POST", "/api/v1/content/write", body)
		if err2 != nil {
			return fmt.Errorf("写入上下文(replace)失败: %w", err2)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode >= 400 {
			respBody, _ := io.ReadAll(resp2.Body)
			return fmt.Errorf("OpenViking 写入(replace)失败: %d %s", resp2.StatusCode, string(respBody))
		}
		return nil
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenViking 写入失败: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

// ReadContext 读取上下文资源（content/read API）。
// 使用 GET /api/v1/content/read?uri=...
// 无扩展名的路径自动追加 .md（与 WriteContext 一致）。
func (c *OpenVikingClient) ReadContext(ctx context.Context, path string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("OpenViking 不可用")
	}

	uri := toVikingURI(path)
	if filepath.Ext(path) == "" {
		uri += ".md"
	}
	url := fmt.Sprintf("%s/api/v1/content/read?uri=%s", c.baseURL, uri)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.markDisabled()
		return "", fmt.Errorf("读取上下文失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("上下文不存在: %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenViking 读取失败: %d %s", resp.StatusCode, string(body))
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析标准响应格式 {"status":"ok","result":{"content":"..."}}
	var apiResp openVikingResponse
	if err := json.Unmarshal(buf.Bytes(), &apiResp); err != nil {
		// 可能直接是文本内容
		return buf.String(), nil
	}
	if apiResp.Status == "ok" {
		if content, ok := apiResp.Result["content"].(string); ok {
			return content, nil
		}
		// 返回整个 result 的 JSON
		resultJSON, _ := json.Marshal(apiResp.Result)
		return string(resultJSON), nil
	}
	return buf.String(), nil
}

// ListDir 列出目录内容（fs/ls API）。
func (c *OpenVikingClient) ListDir(ctx context.Context, path string) ([]string, error) {
	if !c.IsEnabled() {
		return nil, fmt.Errorf("OpenViking 不可用")
	}

	uri := toVikingURI(path)
	url := fmt.Sprintf("%s/api/v1/fs/ls?uri=%s", c.baseURL, uri)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.markDisabled()
		return nil, fmt.Errorf("列出目录失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("OpenViking ls 失败: %d", resp.StatusCode)
	}

	var apiResp openVikingResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	var entries []string
	if entriesRaw, ok := apiResp.Result["entries"].([]interface{}); ok {
		for _, e := range entriesRaw {
			if m, ok := e.(map[string]interface{}); ok {
				if name, ok := m["name"].(string); ok {
					entries = append(entries, name)
				}
			}
		}
	}
	return entries, nil
}

// Mkdir 创建目录（fs/mkdir API）。
func (c *OpenVikingClient) Mkdir(ctx context.Context, path string) error {
	if !c.IsEnabled() {
		return nil
	}

	uri := toVikingURI(path)
	body := map[string]interface{}{"uri": uri}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/fs/mkdir", body)
	if err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenViking mkdir 失败: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

// Delete 删除资源（DELETE /api/v1/fs）。
func (c *OpenVikingClient) Delete(ctx context.Context, path string) error {
	if !c.IsEnabled() {
		return nil
	}

	uri := toVikingURI(path)
	url := fmt.Sprintf("%s/api/v1/fs?uri=%s", c.baseURL, uri)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("删除资源失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("OpenViking delete 失败: %d", resp.StatusCode)
	}
	return nil
}

// --- 语义搜索 ---

// Find 语义搜索（search/find API）。
// query 为自然语言查询，targetURI 限定搜索范围（可选）。
func (c *OpenVikingClient) Find(ctx context.Context, query string, targetURI string) ([]OpenVikingSearchResult, error) {
	if !c.IsEnabled() {
		return nil, fmt.Errorf("OpenViking 不可用")
	}

	body := map[string]interface{}{
		"query": query,
	}
	if targetURI != "" {
		body["target_uri"] = toVikingURI(targetURI)
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/search/find", body)
	if err != nil {
		return nil, fmt.Errorf("语义搜索失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenViking find 失败: %d %s", resp.StatusCode, string(respBody))
	}

	var apiResp openVikingResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	var results []OpenVikingSearchResult
	if resources, ok := apiResp.Result["resources"].([]interface{}); ok {
		for _, r := range resources {
			if m, ok := r.(map[string]interface{}); ok {
				result := OpenVikingSearchResult{
					URI:   getString(m, "uri"),
					Score: getFloat(m, "score"),
				}
				results = append(results, result)
			}
		}
	}
	return results, nil
}

// OpenVikingSearchResult 语义搜索结果。
type OpenVikingSearchResult struct {
	URI   string  `json:"uri"`
	Score float64 `json:"score"`
}

// --- 会话管理 ---

// CreateSession 创建会话。
func (c *OpenVikingClient) CreateSession(ctx context.Context) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("OpenViking 不可用")
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/sessions", map[string]interface{}{})
	if err != nil {
		return "", fmt.Errorf("创建会话失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("创建会话失败: %d", resp.StatusCode)
	}

	var apiResp openVikingResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if id, ok := apiResp.Result["id"].(string); ok {
		return id, nil
	}
	return "", fmt.Errorf("会话 ID 未返回")
}

// AddSessionMessage 向会话添加消息。
func (c *OpenVikingClient) AddSessionMessage(ctx context.Context, sessionID, role, content string) error {
	if !c.IsEnabled() {
		return nil
	}

	body := map[string]interface{}{
		"role":    role,
		"content": content,
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/sessions/%s/messages", sessionID), body)
	if err != nil {
		return fmt.Errorf("添加消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("添加消息失败: %d", resp.StatusCode)
	}
	return nil
}

// CommitSession 提交会话（归档并提取记忆）。
func (c *OpenVikingClient) CommitSession(ctx context.Context, sessionID string) error {
	if !c.IsEnabled() {
		return nil
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/api/v1/sessions/%s/commit", sessionID), map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("提交会话失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("提交会话失败: %d", resp.StatusCode)
	}
	return nil
}

// GetSessionContext 获取会话上下文。
func (c *OpenVikingClient) GetSessionContext(ctx context.Context, sessionID string) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("OpenViking 不可用")
	}

	url := fmt.Sprintf("%s/api/v1/sessions/%s/context", c.baseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取会话上下文失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("获取会话上下文失败: %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String(), nil
}

// --- 便捷方法 ---

// UpdateMemory 更新长期记忆（写入 user/memories 目录）。
// namespace 用于隔离不同会话的记忆，key 是记忆条目标识。
func (c *OpenVikingClient) UpdateMemory(ctx context.Context, namespace, key, content string) error {
	path := fmt.Sprintf("user/memories/%s/%s", namespace, key)
	return c.WriteContext(ctx, path, content)
}

// ReadMemory 读取长期记忆。
func (c *OpenVikingClient) ReadMemory(ctx context.Context, namespace, key string) (string, error) {
	path := fmt.Sprintf("user/memories/%s/%s", namespace, key)
	return c.ReadContext(ctx, path)
}

// WriteJSON 写入 JSON 格式的上下文。
func (c *OpenVikingClient) WriteJSON(ctx context.Context, path string, data interface{}) error {
	if !c.IsEnabled() {
		return nil
	}
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	return c.WriteContext(ctx, path, string(jsonData))
}

// --- 内部方法 ---

// openVikingResponse 是 OpenViking API 标准响应格式。
type openVikingResponse struct {
	Status string                 `json:"status"`
	Result map[string]interface{} `json:"result,omitempty"`
	Error  *openVikingError       `json:"error,omitempty"`
	Time   float64                `json:"time,omitempty"`
}

type openVikingError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// doRequest 执行 HTTP 请求，自动添加鉴权头和 JSON body。
func (c *OpenVikingClient) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	c.setAuthHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// setAuthHeaders 设置鉴权和多租户请求头。
func (c *OpenVikingClient) setAuthHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if c.account != "" {
		req.Header.Set("X-OpenViking-Account", c.account)
	}
	if c.user != "" {
		req.Header.Set("X-OpenViking-User", c.user)
	}
}

// ping 检测 OpenViking 服务是否可用。
func (c *OpenVikingClient) ping(ctx context.Context) error {
	url := c.baseURL + "/health"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// /health 可能需要认证，尝试 /ready
		url2 := c.baseURL + "/ready"
		req2, _ := http.NewRequestWithContext(ctx, "GET", url2, nil)
		c.setAuthHeaders(req2)
		resp2, err2 := c.httpClient.Do(req2)
		if err2 != nil {
			return fmt.Errorf("健康检查返回: %d", resp.StatusCode)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode >= 400 {
			return fmt.Errorf("健康检查返回: %d / %d", resp.StatusCode, resp2.StatusCode)
		}
	}
	return nil
}

// markDisabled 标记为降级模式。
func (c *OpenVikingClient) markDisabled() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled {
		c.enabled = false
		log.Printf("[OpenViking] 降级为仅本地存储模式")
	}
}

// TryReconnect 尝试重新连接 OpenViking。
func (c *OpenVikingClient) TryReconnect(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.enabled {
		return true
	}

	if err := c.ping(ctx); err != nil {
		return false
	}

	c.enabled = true
	log.Printf("[OpenViking] 重新连接成功: %s", c.baseURL)
	return true
}

// --- 辅助函数 ---

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}
