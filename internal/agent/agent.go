// Package agent 封装 trpc-agent-go 的 AI Agent 能力。
// 实现了 core.AgentHandler 接口，可被 PluginManager 统一调度。
//
// 与 Handler 层解耦：Agent 不直接依赖 TRPG 引擎，
// 而是通过 Session.Data 共享游戏状态（角色卡、骰子结果等）。
package agent

import (
	"fmt"
	"log"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// Config 是 AI Agent 的配置。
type Config struct {
	LLMProvider  string  // deepseek / openai / hunyuan
	LLMModel     string  // 模型名称 (如 deepseek-chat, deepseek-reasoner)
	LLMAPIKey    string  // API 密钥 (DeepSeek: sk-xxx)
	LLMBaseURL   string  // API 地址 (DeepSeek: https://api.deepseek.com)
	MaxTokens    int     // 最大 token 数
	Temperature  float64 // 温度
	MemoryWindow int     // 上下文记忆窗口（消息条数）
	SystemPrompt string  // 系统提示词
}

// DefaultKPPrompt 返回默认的 KP/DM 主持人提示词。
func DefaultKPPrompt() string {
	return `你是一个经验丰富的 TRPG 游戏主持人（KP/DM）。
你负责引导玩家进行桌面角色扮演游戏，包括：
1. 描述场景和氛围
2. 扮演 NPC
3. 根据玩家行动推进剧情
4. 在需要时要求玩家进行骰点判定
请保持沉浸感和趣味性，尊重玩家的选择。

你可以使用 roll_dice 工具来为玩家投掷骰子。
当需要技能检定时，主动调用 roll_dice 工具或 skill_check 工具并告知玩家结果。

【剧本模式约束】
当加载了剧本后，你必须严格遵循剧本的剧情发展，不能随意拓展或编造剧情：
1. 只在剧本设定的时间轴节点内推进故事
2. NPC 的行为和对话必须符合剧本中描述的性格和背景
3. 场景描述应基于剧本中的场景信息，可适当丰富细节但不改变核心内容
4. 当玩家完成当前节点的关键事件后，使用 advance_timeline 工具推进剧情
5. 定期使用 save_progress 工具保存剧情进度摘要
6. 使用 get_script_context 工具查看当前剧本上下文和可推进方向
7. 使用 get_npc 工具获取 NPC 信息以准确扮演角色
8. 如果不确定剧情走向，优先使用 get_progress 和 get_script_context 查看当前状态`
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
