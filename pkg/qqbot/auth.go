package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// tokenRequest 是获取 AccessToken 的请求体。
type tokenRequest struct {
	AppID         string `json:"appId"`
	ClientSecret  string `json:"clientSecret"`
}

// tokenResponse 是获取 AccessToken 的响应体。
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   intOrString `json:"expires_in"` // 有效期，秒
}

// intOrString 能兼容 JSON 中以数字或字符串表示的整数值。
// QQ Bot API 的 expires_in 字段在不同实现中可能返回 int 或 string。
type intOrString int

// UnmarshalJSON 实现 json.Unmarshaler 接口，兼容数字和字符串两种 JSON 表示。
func (n *intOrString) UnmarshalJSON(data []byte) error {
	s := string(data)
	// 尝试直接解析为数字
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		*n = intOrString(i)
		return nil
	}

	// 尝试解析为带引号的字符串
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("无法将 %s 解析为整数: %w", s, err)
	}
	i, err = strconv.ParseInt(str, 10, 64)
	if err != nil {
		return fmt.Errorf("无法将字符串 %q 解析为整数: %w", str, err)
	}
	*n = intOrString(i)
	return nil
}

// tokenManager 管理 AccessToken 的获取和自动刷新。
type tokenManager struct {
	appID        string
	clientSecret string
	httpClient   *http.Client

	mu          sync.RWMutex
	accessToken string
	expiresAt   time.Time // token 过期时间
}

// newTokenManager 创建 Token 管理器。
func newTokenManager(appID, clientSecret string) *tokenManager {
	return &tokenManager{
		appID:        appID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// getToken 获取有效的 AccessToken，过期时自动刷新。
// 在过期前60秒内刷新时，旧 token 仍然有效（官方平滑过渡机制）。
func (m *tokenManager) getToken(ctx context.Context) (string, error) {
	m.mu.RLock()
	if m.accessToken != "" && time.Now().Add(60*time.Second).Before(m.expiresAt) {
		token := m.accessToken
		m.mu.RUnlock()
		return token, nil
	}
	m.mu.RUnlock()

	return m.refreshToken(ctx)
}

// refreshToken 强制刷新 AccessToken。
func (m *tokenManager) refreshToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查：其他协程可能已经刷新
	if m.accessToken != "" && time.Now().Add(60*time.Second).Before(m.expiresAt) {
		return m.accessToken, nil
	}

	reqBody, err := json.Marshal(tokenRequest{
		AppID:        m.appID,
		ClientSecret: m.clientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("序列化 token 请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AccessTokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("创建 token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 token 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("获取 token 失败, HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("解析 token 响应失败: %w", err)
	}

	m.accessToken = tokenResp.AccessToken
	// 提前60秒过期，留出平滑过渡窗口
	m.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	return m.accessToken, nil
}

// authHeader 返回标准的鉴权请求头值，格式 "QQBot {accessToken}"。
func (m *tokenManager) authHeader(ctx context.Context) (string, error) {
	token, err := m.getToken(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s", TokenType, token), nil
}
