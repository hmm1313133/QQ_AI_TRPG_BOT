// Package agent - MemoryService 记忆服务。
//
// 设计文档第五章：事件驱动的记忆写入（异步）+ 三因子检索注入上下文。
//   - AfterTurn: 回合结束后将新事件（EventLog 增量 + 玩家行动）写入记忆，异步执行
//   - BuildMemoryBlock: 每回合检索 top-K 记忆，组装 NPC 认知卡注入 Narrator
//   - Reflector: 有效记忆超阈值时压缩为高层洞察（带证据引用），异步执行
//   - OpenViking 启用时：语义检索替代本地 bigram，记忆双写
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/store"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 记忆写入的重要性评分规则（设计文档 5.2：规则化优先）。
const (
	importanceDecision   = 7
	importanceDeath      = 10
	importanceHidden     = 6
	importanceEvent      = 6
	importanceObjective  = 7
	importanceRelation   = 5
	importancePlayerAct  = 3
	importanceReflection = 8
)

// reflectThreshold 触发反思压缩的有效记忆条数阈值。
const reflectThreshold = 30

// memoryTopK 每 NPC / 世界 每轮检索的记忆条数。
const memoryTopK = 3

// MemoryService 记忆服务。
// 面向框架 memory.Service 接口（backend），本地 JSON 持久化；
// 检索用自研三因子重排（世界时间语义），写入双轨：
// 状态事件走规则（零 LLM），对话内容走框架 extractor（LLM 抽取去重）。
type MemoryService struct {
	store    *world.MemoryStore
	backend  memory.Service // 框架 memory.Service 接口（JSON 后端）
	extractor extractor.MemoryExtractor // 框架对话记忆抽取器（可为 nil 禁用）
	ov       *store.OpenVikingClient
	director *Director // Reflector 压缩用（低温度 runner，可为 nil 降级规则压缩）

	mu         sync.Mutex
	reflecting map[string]bool // 防并发反思: worldID/entity
}

// NewMemoryService 创建记忆服务。
// extractor 可为 nil（禁用对话抽取，仅规则化写入）。
func NewMemoryService(store *world.MemoryStore, ov *store.OpenVikingClient, director *Director,
	worldEngine *world.Engine, ext extractor.MemoryExtractor) *MemoryService {
	backend := NewJSONMemoryService(store, func(worldID string) int64 {
		if worldEngine != nil {
			if ws := worldEngine.LoadOrNil(worldID); ws != nil {
				return ws.Clock.WorldTime
			}
		}
		return time.Now().Unix() / 60
	})
	return &MemoryService{
		store:      store,
		backend:    backend,
		extractor:  ext,
		ov:         ov,
		director:   director,
		reflecting: make(map[string]bool),
	}
}

// worldMemoryEntity 世界级记忆的实体名。
const worldMemoryEntity = "_world"

// TRPGMemoryExtractPrompt 是对话记忆抽取器的自定义 prompt（框架 extractor）。
const TRPGMemoryExtractPrompt = `你是 TRPG 跑团游戏的记忆提取器。从主持人（KP）与玩家的对话回合中，抽取值得长期记住的事实：
- 玩家做出的重要决定、行动及其后果
- NPC 透露的关键信息、NPC 与玩家关系的变化
- 获得的物品、发现的线索、解锁的地点或区域
- 世界状态的重要变化（地点、势力、事件）
忽略：纯粹的寒暄、骰点过程描述、与剧情无关的闲聊。
每条记忆用一句简洁的第三人称中文陈述，包含关键主体与结果。`

// ============================================================
// 写入路径（异步）
// ============================================================

// AfterTurn 回合结束后的记忆写入（异步 goroutine 调用）。
// newEvents 为本回合 EventLog 增量；playerMessage/narratorReply 为本回合对话。
// 双轨写入：状态事件走规则（零 LLM），对话内容走框架 extractor（LLM 抽取去重）。
func (m *MemoryService) AfterTurn(state *world.WorldState, playerMessage, narratorReply string, newEvents []world.WorldEvent) {
	if m.store == nil || state == nil {
		return
	}
	worldID := state.WorldID
	now := state.Clock.WorldTime

	// 1. 玩家行动 → 世界记忆（低重要性流水账，供反思压缩）
	if strings.TrimSpace(playerMessage) != "" {
		m.addMemory(worldID, worldMemoryEntity, world.MemoryEntry{
			Content:    fmt.Sprintf("玩家行动: %s", truncate(playerMessage, 100)),
			Importance: importancePlayerAct,
			WorldTime:  now,
			LastAccess: now,
		})
	}

	// 2. 事件 → 相关实体记忆（规则化，零 LLM）
	for _, ev := range newEvents {
		content, importance, pinned := memoryFromEvent(ev)
		if content == "" {
			continue
		}
		entry := world.MemoryEntry{
			Content:    content,
			Importance: importance,
			Pinned:     pinned,
			WorldTime:  now,
			LastAccess: now,
			Tags:       []string{ev.Type, ev.Target},
		}
		// 世界记忆
		m.addMemory(worldID, worldMemoryEntity, entry)
		// 涉及的 NPC 记忆（target 能匹配到角色时）
		if npc := findNPCByTarget(state, ev.Target); npc != nil {
			m.addMemory(worldID, npc.Name, entry)
		}
	}

	// 3. 对话 → 框架 extractor 抽取（LLM，带去重；仅世界记忆实体）
	if m.extractor != nil && strings.TrimSpace(playerMessage) != "" {
		m.extractFromDialogue(state, playerMessage, narratorReply)
	}

	// 4. 反思阈值检查（异步，防重入）
	for _, entity := range m.entitiesToCheck(state) {
		if m.store.Count(worldID, entity) > reflectThreshold {
			go m.reflectSafe(worldID, entity, now)
		}
	}
}

// extractFromDialogue 用框架 extractor 从本回合对话中抽取记忆。
// extractor 自带对现有记忆的去重（add/update/delete 操作集）。
func (m *MemoryService) extractFromDialogue(state *world.WorldState, playerMessage, narratorReply string) {
	ctx := context.Background()
	userKey := memory.UserKey{AppName: state.WorldID, UserID: worldMemoryEntity}

	messages := []model.Message{
		model.NewUserMessage(truncate(playerMessage, 500)),
	}
	if strings.TrimSpace(narratorReply) != "" {
		messages = append(messages, model.NewAssistantMessage(truncate(narratorReply, 1000)))
	}

	existing, err := m.backend.ReadMemories(ctx, userKey, 20)
	if err != nil {
		log.Printf("[Memory] 读取现有记忆失败（抽取跳过）: %v", err)
		return
	}

	ops, err := m.extractor.Extract(ctx, messages, existing)
	if err != nil {
		log.Printf("[Memory] 对话记忆抽取失败: %v", err)
		return
	}

	for _, op := range ops {
		if op == nil {
			continue
		}
		switch op.Type {
		case extractor.OperationAdd:
			_ = m.backend.AddMemory(ctx, userKey, op.Memory, op.Topics)
		case extractor.OperationUpdate:
			_ = m.backend.UpdateMemory(ctx, memory.Key{
				AppName: userKey.AppName, UserID: userKey.UserID, MemoryID: op.MemoryID,
			}, op.Memory, op.Topics)
		case extractor.OperationDelete:
			_ = m.backend.DeleteMemory(ctx, memory.Key{
				AppName: userKey.AppName, UserID: userKey.UserID, MemoryID: op.MemoryID,
			})
		case extractor.OperationClear:
			_ = m.backend.ClearMemories(ctx, userKey)
		}
	}
	if len(ops) > 0 {
		log.Printf("[Memory] 对话抽取完成: %d 个操作", len(ops))
	}
}

// addMemory 写入一条记忆（OpenViking 启用时双写）。
func (m *MemoryService) addMemory(worldID, entity string, entry world.MemoryEntry) {
	entry.ID = fmt.Sprintf("mem_%d", time.Now().UnixNano())
	if err := m.store.Append(worldID, entity, entry); err != nil {
		log.Printf("[Memory] 写入记忆失败: world=%s entity=%s: %v", worldID, entity, err)
		return
	}
	if m.ov != nil && m.ov.IsEnabled() {
		path := fmt.Sprintf("memories/%s/%s/%s", worldID, entity, entry.ID)
		_ = m.ov.WriteJSON(context.Background(), path, entry)
	}
}

// memoryFromEvent 将世界事件转换为记忆条目（内容/重要性/是否里程碑）。
func memoryFromEvent(ev world.WorldEvent) (string, int, bool) {
	switch ev.Type {
	case "decision":
		return fmt.Sprintf("玩家的关键决策: %s", ev.Value), importanceDecision, true
	case "npc_disposition":
		if ev.Value == "dead" {
			return fmt.Sprintf("%s 死亡（%s）", ev.Target, ev.CausedBy), importanceDeath, true
		}
		return fmt.Sprintf("%s 的态度变为 %s", ev.Target, ev.Value), importanceRelation, false
	case "hidden_discovered":
		return fmt.Sprintf("玩家发现了线索: %s", ev.Target), importanceHidden, false
	case "event_triggered":
		return fmt.Sprintf("事件被触发: %s", ev.Target), importanceEvent, false
	case "objective_completed":
		return fmt.Sprintf("目标完成: %s", ev.Target), importanceObjective, false
	case "mood_change", "relation_change":
		return "", 0, false // 情绪/关系是状态而非记忆，不重复记录
	default:
		return "", 0, false
	}
}

// findNPCByTarget 在角色表中匹配事件目标。
func findNPCByTarget(state *world.WorldState, target string) *world.CharacterState {
	for _, c := range state.Characters {
		if c.Name == target || strings.Contains(target, c.Name) {
			return c
		}
	}
	return nil
}

// entitiesToCheck 需要检查反思阈值的实体列表（世界 + 活跃 NPC）。
func (m *MemoryService) entitiesToCheck(state *world.WorldState) []string {
	entities := []string{worldMemoryEntity}
	for _, c := range state.Characters {
		if c.Alive {
			entities = append(entities, c.Name)
		}
	}
	return entities
}

// ============================================================
// 检索路径（同步，注入上下文包）
// ============================================================

// BuildMemoryBlock 检索相关记忆，组装注入 Narrator 的记忆块。
// query 通常为玩家当前输入。
func (m *MemoryService) BuildMemoryBlock(state *world.WorldState, query string) string {
	if m.store == nil || state == nil {
		return ""
	}
	worldID := state.WorldID
	now := state.Clock.WorldTime

	var sb strings.Builder

	// 世界记忆
	if block := m.retrieveBlock(worldID, worldMemoryEntity, "世界", query, now); block != "" {
		sb.WriteString(block)
	}

	// 活跃 NPC 记忆
	for _, npc := range state.Characters {
		if !npc.Alive {
			continue
		}
		if block := m.retrieveBlock(worldID, npc.Name, npc.Name, query, now); block != "" {
			sb.WriteString(block)
		}
	}

	if sb.Len() == 0 {
		return ""
	}
	return "【相关记忆】\n" + sb.String()
}

// retrieveBlock 检索单个实体的 top-K 记忆并格式化。
func (m *MemoryService) retrieveBlock(worldID, entity, label, query string, now int64) string {
	entries, err := m.store.List(worldID, entity)
	if err != nil || len(entries) == 0 {
		return ""
	}

	top := world.Retrieve(entries, query, now, memoryTopK, m.relevanceFunc(worldID, entity))
	if len(top) == 0 {
		return ""
	}

	// 持久化 LastAccess 更新（保鲜）
	_ = m.store.SaveAll(worldID, entity, entries)

	var sb strings.Builder
	for _, e := range top {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", label, e.Content))
	}
	return sb.String()
}

// relevanceFunc 语义相关度函数。
// 框架迁移后，检索后端统一走 memory.Service（JSON 后端用本地 bigram，
// 升级 mem0 后端后获得向量语义检索），OpenViking Find 路径已废弃，
// 此处恒返回 nil（使用本地 bigram）。
func (m *MemoryService) relevanceFunc(worldID, entity string) world.RelevanceFunc {
	return nil
}

// ============================================================
// 反思压缩（异步）
// ============================================================

// reflectSafe 防重入的反思入口。
func (m *MemoryService) reflectSafe(worldID, entity string, now int64) {
	key := worldID + "/" + entity
	m.mu.Lock()
	if m.reflecting[key] {
		m.mu.Unlock()
		return
	}
	m.reflecting[key] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.reflecting, key)
		m.mu.Unlock()
	}()

	if err := m.reflect(worldID, entity, now); err != nil {
		log.Printf("[Memory] 反思压缩失败: %s: %v", key, err)
	}
}

// reflect 将实体的有效记忆压缩为高层洞察。
// LLM 可用时生成带证据引用的洞察；失败时降级为规则压缩
// （归档低重要性非 Pinned 条目）。
func (m *MemoryService) reflect(worldID, entity string, now int64) error {
	entries, err := m.store.List(worldID, entity)
	if err != nil || len(entries) == 0 {
		return err
	}

	// 收集有效条目（Pinned 永不压缩）
	var activeIdx []int
	for i := range entries {
		if !entries[i].Invalid && !entries[i].Pinned {
			activeIdx = append(activeIdx, i)
		}
	}
	if len(activeIdx) <= reflectThreshold/2 {
		return nil
	}

	// 尝试 LLM 反思（生成 1-2 条高层洞察，带证据引用）
	reflected := false
	if m.director != nil {
		if insight, evidenceIDs, err := m.llmReflect(worldID, entity, entries, activeIdx); err == nil && insight != "" {
			m.addMemory(worldID, entity, world.MemoryEntry{
				Content:    "【反思】" + insight,
				Importance: importanceReflection,
				WorldTime:  now,
				LastAccess: now,
				Evidence:   evidenceIDs,
			})
			reflected = true
		} else if err != nil {
			log.Printf("[Memory] LLM 反思失败，降级规则压缩: %v", err)
		}
	}

	// 无论 LLM 是否成功，都归档已压缩的低重要性条目（失效不删除）
	archived := 0
	for _, i := range activeIdx {
		if entries[i].Importance <= 4 {
			entries[i].Invalid = true
			archived++
		}
	}
	if err := m.store.SaveAll(worldID, entity, entries); err != nil {
		return err
	}

	log.Printf("[Memory] 反思完成: %s/%s, LLM洞察=%v, 归档=%d", worldID, entity, reflected, archived)
	return nil
}

// llmReflect 调用低温度 LLM 生成反思洞察。
func (m *MemoryService) llmReflect(worldID, entity string, entries []world.MemoryEntry, activeIdx []int) (string, []string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("以下是角色「%s」的记忆列表：\n", entity))
	idList := make([]string, 0, len(activeIdx))
	for _, i := range activeIdx {
		e := entries[i]
		sb.WriteString(fmt.Sprintf("[%s] (重要性%d) %s\n", e.ID, e.Importance, e.Content))
		idList = append(idList, e.ID)
	}
	sb.WriteString("\n请总结 1-2 条关于这个角色（或ta与玩家关系）的高层洞察，" +
		"并在末尾用 JSON 格式给出支撑证据的记忆ID列表，格式：\n洞察文本\n{\"evidence\": [\"id1\", \"id2\"]}")

	reply, err := m.director.rawCompletion(worldID, "reflector", sb.String())
	if err != nil {
		return "", nil, err
	}

	// 解析洞察与证据
	insight := reply
	var evidence []string
	if idx := strings.LastIndex(reply, "{\"evidence\""); idx >= 0 {
		insight = strings.TrimSpace(reply[:idx])
		jsonStr := extractAgentJSON(reply[idx:])
		var parsed struct {
			Evidence []string `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			evidence = parsed.Evidence
		}
	}
	if insight == "" {
		return "", nil, fmt.Errorf("LLM 反思返回空洞察")
	}
	return insight, evidence, nil
}
