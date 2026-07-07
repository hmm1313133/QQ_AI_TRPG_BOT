package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAPI 是 QQ 机器人 HTTP API 客户端。
// 文档: https://bot.q.qq.com/wiki/develop/api-v2/server-inter/
type OpenAPI struct {
	tokenMgr   *tokenManager
	httpClient *http.Client
}

// newOpenAPI 创建 OpenAPI 客户端。
func newOpenAPI(tokenMgr *tokenManager) *OpenAPI {
	return &OpenAPI{
		tokenMgr:   tokenMgr,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// apiError 是 API 错误响应。
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error 实现 error 接口。
func (e *apiError) Error() string {
	return fmt.Sprintf("QQ Bot API 错误 [code=%d]: %s", e.Code, e.Message)
}

// doRequest 执行 HTTP 请求，自动添加鉴权头。
func (a *OpenAPI) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	auth, err := a.tokenMgr.authHeader(ctx)
	if err != nil {
		return fmt.Errorf("获取鉴权信息失败: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := APIBaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// 非 2xx 状态码，尝试解析错误
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		if json.Unmarshal(respData, &apiErr) == nil && apiErr.Code != 0 {
			return &apiErr
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respData))
	}

	// 解析成功响应
	if result != nil && len(respData) > 0 {
		if err := json.Unmarshal(respData, result); err != nil {
			return fmt.Errorf("解析响应失败: %w, body: %s", err, string(respData))
		}
	}

	return nil
}

// doGET 执行 GET 请求。
func (a *OpenAPI) doGET(ctx context.Context, path string, result interface{}) error {
	return a.doRequest(ctx, http.MethodGet, path, nil, result)
}

// doPOST 执行 POST 请求。
func (a *OpenAPI) doPOST(ctx context.Context, path string, body, result interface{}) error {
	return a.doRequest(ctx, http.MethodPost, path, body, result)
}

// doPUT 执行 PUT 请求。
func (a *OpenAPI) doPUT(ctx context.Context, path string, body, result interface{}) error {
	return a.doRequest(ctx, http.MethodPut, path, body, result)
}

// doDELETE 执行 DELETE 请求。
func (a *OpenAPI) doDELETE(ctx context.Context, path string, result interface{}) error {
	return a.doRequest(ctx, http.MethodDelete, path, nil, result)
}

// doPATCH 执行 PATCH 请求。
func (a *OpenAPI) doPATCH(ctx context.Context, path string, body, result interface{}) error {
	return a.doRequest(ctx, http.MethodPatch, path, body, result)
}

// GatewayBot 返回 WSS 接入点和分片信息。
// 文档: /gateway/bot
type GatewayBot struct {
	URL    string `json:"url"`    // WSS 接入地址
	Shards int    `json:"shards"` // 建议分片数
	SessionStartLimit struct {
		Total          int `json:"total"`           // 总连接数
		Remaining      int `json:"remaining"`       // 剩余连接数
		ResetAfter     int `json:"reset_after"`     // 重置时间（毫秒）
		MaxConcurrency int `json:"max_concurrency"` // 最大并发连接数
	} `json:"session_start_limit"`
}

// GetGatewayBot 获取带分片信息的 WSS 接入点。
func (a *OpenAPI) GetGatewayBot(ctx context.Context) (*GatewayBot, error) {
	var result GatewayBot
	if err := a.doGET(ctx, "/gateway/bot", &result); err != nil {
		return nil, fmt.Errorf("获取 gateway 失败: %w", err)
	}
	return &result, nil
}

// GetGateway 获取通用 WSS 接入点（不带分片信息）。
func (a *OpenAPI) GetGateway(ctx context.Context) (string, error) {
	var result struct {
		URL string `json:"url"`
	}
	if err := a.doGET(ctx, "/gateway", &result); err != nil {
		return "", fmt.Errorf("获取 gateway 失败: %w", err)
	}
	return result.URL, nil
}
