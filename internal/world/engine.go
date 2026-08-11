// 世界引擎：状态变更的唯一写入入口（设计文档原则 3：单一写入者）。
//
// 所有状态变更必须经过 ApplyEvent：LLM 的工具调用、Director/Planner 的
// state_requests、规则层的副作用，统一在这里校验（状态锁定、目标匹配）
// 并追加到 EventLog（事件溯源）。
package world

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// Engine 世界引擎。
type Engine struct {
	repo StateRepository

	consequence ConsequenceEngine

	// 会话级互斥锁：串行化同一世界的回合处理与状态读写。
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewEngine 创建世界引擎。
func NewEngine(repo StateRepository) *Engine {
	return &Engine{
		repo:        repo,
		consequence: NewRuleConsequence(),
		locks:       make(map[string]*sync.Mutex),
	}
}

// SetConsequence 注入后果传播引擎。
func (e *Engine) SetConsequence(c ConsequenceEngine) {
	if c != nil {
		e.consequence = c
	}
}

// Repo 返回底层存储（供迁移工具等使用）。
func (e *Engine) Repo() StateRepository { return e.repo }

// Lock 锁定指定世界（回合处理期间持有，防止并发读写）。
func (e *Engine) Lock(worldID string) { e.worldLock(worldID).Lock() }

// Unlock 解锁指定世界。
func (e *Engine) Unlock(worldID string) { e.worldLock(worldID).Unlock() }

func (e *Engine) worldLock(worldID string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.locks[worldID]
	if !ok {
		l = &sync.Mutex{}
		e.locks[worldID] = l
	}
	return l
}

// Load 加载世界状态。
// 加载后执行旧 Background 的 lore 兼容迁移（内存中转换，下次 Save 落盘）。
func (e *Engine) Load(worldID string) (*WorldState, error) {
	state, err := e.repo.Load(worldID)
	if err != nil {
		return nil, err
	}
	MigrateLegacyBackground(state)
	return state, nil
}

// LoadOrNil 加载世界状态，不存在返回 nil。
func (e *Engine) LoadOrNil(worldID string) *WorldState {
	state, err := e.repo.Load(worldID)
	if err != nil {
		return nil
	}
	MigrateLegacyBackground(state)
	return state
}

// Save 保存世界状态。
func (e *Engine) Save(state *WorldState) error {
	return e.repo.Save(state)
}

// Delete 删除世界状态。
func (e *Engine) Delete(worldID string) error {
	return e.repo.Delete(worldID)
}

// ============================================================
// 回合快照（"AI 回复重试"：恢复快照 + 用原用户输入重跑）
// ============================================================

// SaveTurnSnapshot 保存回合前快照（仅 SQLite 存储支持，其他实现静默跳过）。
// 调用方须已持有世界锁（TurnEngine 回合内调用，整个 Run 持锁）。
func (e *Engine) SaveTurnSnapshot(worldID string, state *WorldState, userText string) error {
	repo, ok := e.repo.(*SQLiteRepository)
	if !ok {
		return nil
	}
	return repo.SaveTurnSnapshot(worldID, state, userText)
}

// RestoreTurnSnapshot 恢复回合快照（覆盖当前世界状态），返回当时的用户输入。
// 无快照（normal 模式 / 老数据 / 非 SQLite 存储）时 ok=false，调用方走兜底重跑。
func (e *Engine) RestoreTurnSnapshot(worldID string) (userText string, ok bool, err error) {
	repo, castOK := e.repo.(*SQLiteRepository)
	if !castOK {
		return "", false, nil
	}
	e.Lock(worldID)
	defer e.Unlock(worldID)
	return repo.RestoreTurnSnapshot(worldID)
}

// UpdateRecentNarrator 把 RecentTurns 窗口内最后一处 Narrator == oldText 替换为 newText
// 并落库（编辑/版本切换后同步 LLM 近期对话上下文；窗口内未命中则静默跳过，尽力而为）。
// 已知限制：已并入 PendingSummaryTurns / CampaignSummary 的文本不回改。
func (e *Engine) UpdateRecentNarrator(worldID, oldText, newText string) error {
	if oldText == "" || oldText == newText {
		return nil
	}
	e.Lock(worldID)
	defer e.Unlock(worldID)
	ws := e.LoadOrNil(worldID)
	if ws == nil {
		return nil
	}
	for i := len(ws.RecentTurns) - 1; i >= 0; i-- {
		if ws.RecentTurns[i].Narrator == oldText {
			ws.RecentTurns[i].Narrator = newText
			return e.repo.Save(ws)
		}
	}
	return nil
}

// ListWorlds 列出所有世界 ID。
func (e *Engine) ListWorlds() ([]string, error) {
	return e.repo.List()
}

// ============================================================
// 存档恢复（管理端/玩家端/QQ 指令共用的恢复核心）
// ============================================================

// RestoreSave 恢复存档：校验归属 → 世界锁内自动备份当前进度 → 快照记恢复事件 → 覆盖落库。
// 返回存档元数据与对话历史快照（无快照为 nil；历史回放由调用方按渠道决定）。
func (e *Engine) RestoreSave(worldID string, saveID int64, actor string) (*SaveInfo, []byte, error) {
	repo, ok := e.repo.(*SQLiteRepository)
	if !ok {
		return nil, nil, fmt.Errorf("存档功能需要 SQLite 存储")
	}
	info, snapshot, history, err := repo.LoadSave(saveID)
	if err != nil {
		return nil, nil, fmt.Errorf("存档不存在")
	}
	if info.WorldID != worldID {
		return nil, nil, fmt.Errorf("存档不属于该世界")
	}

	e.Lock(worldID)
	defer e.Unlock(worldID)

	// 恢复前自动备份当前进度
	if current := e.LoadOrNil(worldID); current != nil {
		if _, err := repo.CreateSave(current, "自动备份-恢复前", "恢复存档「"+info.Name+"」前的进度", true); err != nil {
			return nil, nil, fmt.Errorf("自动备份失败: %w", err)
		}
	}

	if _, err := e.ApplyEvent(snapshot, WorldEvent{
		Type: "note", Actor: actor, Target: "save:restore",
		Value: fmt.Sprintf("从存档「%s」恢复（原存档轮次 %d）", info.Name, info.RoundCount),
	}); err != nil {
		return nil, nil, fmt.Errorf("恢复失败: %w", err)
	}
	if err := e.Save(snapshot); err != nil {
		return nil, nil, fmt.Errorf("保存失败: %w", err)
	}
	return info, history, nil
}

// ============================================================
// ApplyEvent：状态变更唯一入口
// ============================================================

// ApplyEvent 校验并应用一个状态变更事件，返回是否命中目标。
// 校验规则：
//   - 被 StateLock 锁定的目标拒绝变更（如已死 NPC 不能改态度）
//   - 目标必须存在（ID 或描述模糊匹配）
// 命中的事件追加到 EventLog（事件溯源）。
func (e *Engine) ApplyEvent(ws *WorldState, ev WorldEvent) (bool, error) {
	if ws == nil {
		return false, fmt.Errorf("WorldState 为 nil")
	}
	ev.Round = ws.RoundCount
	ev.Time = ws.Clock.WorldTime

	matched := false
	switch ev.Type {
	case "npc_disposition":
		matched = e.applyNPCDisposition(ws, ev)
	case "hidden_discovered":
		matched = e.applyHiddenDiscovered(ws, ev)
	case "event_triggered":
		matched = e.applyEventTriggered(ws, ev)
	case "objective_completed":
		matched = e.applyObjectiveCompleted(ws, ev)
	case "mood_change":
		matched = e.applyMoodChange(ws, ev)
	case "relation_change":
		matched = e.applyRelationChange(ws, ev)
	case "lock":
		ws.AddLock(ev.Target, ev.Value, ws.RoundCount)
		matched = true
	case "unlock":
		for i, l := range ws.Locks {
			if l.Key == ev.Target {
				ws.Locks = append(ws.Locks[:i], ws.Locks[i+1:]...)
				matched = true
				break
			}
		}
	case "fact_add":
		e.applyFactAdd(ws, ev)
		matched = true
	case "fact_invalidate":
		matched = e.applyFactInvalidate(ws, ev)
	case "decision", "note":
		matched = true // 纯日志型事件
	default:
		return false, fmt.Errorf("未知事件类型: %s", ev.Type)
	}

	if matched {
		ws.EventLog = append(ws.EventLog, ev)
		if e.consequence != nil {
			e.consequence.Propagate(ws, ev)
		}
	}
	return matched, nil
}

// ApplyEvents 批量应用，返回命中条数。
func (e *Engine) ApplyEvents(ws *WorldState, evs []WorldEvent) int {
	applied := 0
	for _, ev := range evs {
		ok, err := e.ApplyEvent(ws, ev)
		if err != nil {
			log.Printf("[World] ApplyEvent 拒绝: %s target=%s: %v", ev.Type, ev.Target, err)
			continue
		}
		if ok {
			applied++
		} else {
			log.Printf("[World] ApplyEvent 未命中: %s target=%s", ev.Type, ev.Target)
		}
	}
	return applied
}

// applyNPCDisposition 修改 NPC 态度。死亡锁定校验。
func (e *Engine) applyNPCDisposition(ws *WorldState, ev WorldEvent) bool {
	npc := findCharacter(ws, ev.Target)
	if npc == nil {
		return false
	}
	if ws.IsLocked("npc:" + npc.Name + ":dead") {
		log.Printf("[World] 拒绝修改已锁定死亡 NPC %s 的态度", npc.Name)
		return false
	}
	npc.Disposition = ev.Value
	if ev.Value == "dead" {
		npc.Alive = false
		ws.AddLock("npc:"+npc.Name+":dead", ev.CausedBy, ws.RoundCount)
	}
	return true
}

func (e *Engine) applyHiddenDiscovered(ws *WorldState, ev WorldEvent) bool {
	for i := range ws.HiddenInfo {
		h := &ws.HiddenInfo[i]
		if matchTarget(ev.Target, h.ID, h.Description) {
			h.Discovered = true
			return true
		}
	}
	return false
}

func (e *Engine) applyEventTriggered(ws *WorldState, ev WorldEvent) bool {
	for i := range ws.EventQueue {
		pev := &ws.EventQueue[i]
		if matchTarget(ev.Target, pev.ID, pev.Description) {
			pev.Triggered = true
			return true
		}
	}
	return false
}

func (e *Engine) applyObjectiveCompleted(ws *WorldState, ev WorldEvent) bool {
	for i := range ws.Quests {
		q := &ws.Quests[i]
		if matchTarget(ev.Target, "", q.Description) {
			q.Completed = true
			return true
		}
	}
	return false
}

// applyMoodChange 修改 NPC 情绪。Value 格式: "valence=+30,arousal=+10,tag=愤怒"
func (e *Engine) applyMoodChange(ws *WorldState, ev WorldEvent) bool {
	npc := findCharacter(ws, ev.Target)
	if npc == nil || !npc.Alive {
		return false
	}
	applyMoodDelta(&npc.Mood, ev.Value, ws.Clock.WorldTime)
	return true
}

// applyRelationChange 修改关系边。Value 格式: "to=老陈,trust=-50,fear=+30"
func (e *Engine) applyRelationChange(ws *WorldState, ev WorldEvent) bool {
	fields := parseKVFields(ev.Value)
	to := fields["to"]
	if to == "" {
		return false
	}
	rel := ws.GetRelation(ev.Target, to)
	rel.Trust = clamp(rel.Trust+parseIntDefault(fields["trust"], 0), -100, 100)
	rel.Fear = clamp(rel.Fear+parseIntDefault(fields["fear"], 0), -100, 100)
	rel.Debt = clamp(rel.Debt+parseIntDefault(fields["debt"], 0), -100, 100)
	return true
}

// applyFactAdd 添加事实，并使同主谓的旧事实失效（双时序，失效不覆盖）。
func (e *Engine) applyFactAdd(ws *WorldState, ev WorldEvent) {
	// ev.Target = subject, ev.Value = "predicate=状态,object= locked"
	fields := parseKVFields(ev.Value)
	predicate := fields["predicate"]
	object := fields["object"]
	if predicate == "" {
		return
	}
	for i := range ws.Facts {
		f := &ws.Facts[i]
		if f.Subject == ev.Target && f.Predicate == predicate && f.InvalidAt == 0 {
			f.InvalidAt = ws.Clock.WorldTime
		}
	}
	ws.Facts = append(ws.Facts, Fact{
		Subject:    ev.Target,
		Predicate:  predicate,
		Object:     object,
		ValidAt:    ws.Clock.WorldTime,
		RecordedAt: ws.Clock.WorldTime,
	})
}

func (e *Engine) applyFactInvalidate(ws *WorldState, ev WorldEvent) bool {
	for i := range ws.Facts {
		f := &ws.Facts[i]
		if f.Subject == ev.Target && f.InvalidAt == 0 {
			f.InvalidAt = ws.Clock.WorldTime
			return true
		}
	}
	return false
}

// ============================================================
// 目标匹配（ID 精确 + 描述双向子串，≥4 字防误命中）
// ============================================================

// findCharacter 按名称查找角色（精确 + 忽略大小写/空格模糊）。
func findCharacter(ws *WorldState, name string) *CharacterState {
	if c, ok := ws.Characters[name]; ok {
		return c
	}
	trimmed := strings.TrimSpace(name)
	for n, c := range ws.Characters {
		if strings.EqualFold(strings.TrimSpace(n), trimmed) {
			return c
		}
	}
	return nil
}

// FindCharacter 导出供其他包使用。
func (e *Engine) FindCharacter(ws *WorldState, name string) *CharacterState {
	return findCharacter(ws, name)
}

// matchTarget 判断 target 是否命中给定的 id/description。
// 优先精确匹配；target 足够长（≥4 字）时允许双向子串匹配，
// 兼容 LLM 输出描述片段或带前后缀的情况，同时避免过短 target 误命中。
func matchTarget(target, id, description string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if target == id || target == description {
		return true
	}
	if len([]rune(target)) < 4 {
		return false
	}
	if description != "" && (strings.Contains(description, target) || strings.Contains(target, description)) {
		return true
	}
	return false
}

// MatchTarget 导出供测试与其他包使用。
func MatchTarget(target, id, description string) bool {
	return matchTarget(target, id, description)
}

// ============================================================
// 进度与摘要（旧 ProgressTracker 职责）
// ============================================================

// RecordDecision 记录玩家关键决策（追加 decision 事件到 EventLog）。
func (e *Engine) RecordDecision(worldID, action, outcome string) error {
	state, err := e.repo.Load(worldID)
	if err != nil {
		return err
	}
	_, err = e.ApplyEvent(state, WorldEvent{
		Type:   "decision",
		Actor:  "player",
		Target: state.Scene.NodeID,
		Value:  fmt.Sprintf("%s → %s", action, outcome),
	})
	if err != nil {
		return err
	}
	return e.repo.Save(state)
}

// UpdateSummary 更新战役摘要（写入 CampaignSummary，不再覆盖 Background）。
func (e *Engine) UpdateSummary(worldID, summary string) error {
	state, err := e.repo.Load(worldID)
	if err != nil {
		return err
	}
	state.CampaignSummary = summary
	return e.repo.Save(state)
}

// RecentDecisions 返回最近 n 条决策记录（从 EventLog 提取）。
func (e *Engine) RecentDecisions(ws *WorldState, n int) []WorldEvent {
	var decisions []WorldEvent
	for i := len(ws.EventLog) - 1; i >= 0 && len(decisions) < n; i-- {
		if ws.EventLog[i].Type == "decision" {
			decisions = append([]WorldEvent{ws.EventLog[i]}, decisions...)
		}
	}
	return decisions
}
