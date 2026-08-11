// Package agent 封装 trpc-agent-go 的 AI Agent 能力。
// 实现了 core.AgentHandler 接口，可被 PluginManager 统一调度。
//
// Agent 通过 Session.Data 共享会话状态（骰点结果等）。
package agent

import (
	"fmt"
	"log"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// Config 是 AI Agent 的配置。
type Config struct {
	LLMProvider  string  // deepseek / openai / hunyuan
	LLMModel     string  // 模型名称 (如 deepseek-v4-flash, deepseek-reasoner)
	LLMAPIKey    string  // API 密钥 (DeepSeek: sk-xxx)
	LLMBaseURL   string  // API 地址 (DeepSeek: https://api.deepseek.com)
	MaxTokens    int     // 最大 token 数
	Temperature  float64 // 温度
	MemoryWindow int     // 上下文记忆窗口（消息条数）
	SystemPrompt string  // 系统提示词

	// 多层架构配置
	DirectorTemperature float64 // Director 温度（默认 0.2，低温度保证决策一致性）
	DirectorMaxTokens   int     // Director 最大 token 数（默认 2048）
	NarratorTemperature float64 // Narrator 温度（默认 0.7，兼顾创造性和可控性）
	NarratorMaxTokens   int     // Narrator 最大 token 数（默认 4096）
}

// DefaultKPPrompt 返回默认的主提示词（默认预设 + 技术契约段）。
func DefaultKPPrompt() string {
	return ResolveMainPrompt(nil, "")
}

// Manager 管理多个 AI Agent 实例。
type Manager struct {
	agents map[string]core.AgentHandler
}

// NewManager 创建 Agent 管理器。
func NewManager() *Manager {
	return &Manager{
		agents: make(map[string]core.AgentHandler),
	}
}

// RegisterAgent 注册一个已初始化的 Agent。
func (m *Manager) RegisterAgent(id string, agent core.AgentHandler) error {
	if _, exists := m.agents[id]; exists {
		return fmt.Errorf("agent %s 已存在", id)
	}
	m.agents[id] = agent
	log.Printf("[AgentManager] 注册 Agent: %s", id)
	return nil
}

// GetAgent 获取指定 ID 的 Agent。
func (m *Manager) GetAgent(id string) (core.AgentHandler, error) {
	agent, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %s 未注册", id)
	}
	return agent, nil
}

// Agents 返回所有已注册的 Agent ID 列表。
func (m *Manager) Agents() []string {
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	return ids
}
