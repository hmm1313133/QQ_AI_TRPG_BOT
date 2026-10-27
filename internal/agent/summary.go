// Package agent - SummaryService 阶段性剧情摘要服务。
//
// 记忆 = 上下文总结（SillyTavern 式摘要链）：RecentTurns 固定窗口承载近期
// 逐字对话，滑出窗口的对话轮积累到 WorldState.PendingSummaryTurns，攒够
// summary_chunk_size 轮后由本服务异步总结为「一条」阶段摘要（episode 块）。
//
// 落库位置：注入长期记忆存储后直接写 world_memory（直接迁移，不再写
// WorldState.Summaries）；未注入时回退旧字段（JSON 后端/测试）。
// 注入上下文时多条阶段摘要按时间顺序列出，替代远期对话窗口。
// 具体数值/剧情线由世界引擎（EventLog / ScenePlan / Metrics）承载，不进摘要。
package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/memory"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultSummaryChunk 默认待总结批次（轮数）：攒够这么多淘汰轮才触发一次摘要。
const (
	DefaultSummaryChunk = 6
	// DefaultArcWindow 一次"篇章摘要"合并覆盖的 episode 条数（RAPTOR 式层级压缩）。
	DefaultArcWindow  = 6
	SummaryMaxEntries = 64
	SummaryTimeout    = 45 * time.Second
)

// SummaryService 阶段性剧情摘要服务。
type SummaryService struct {
	worldEngine *world.Engine
	// complete 低温度纯文本补全（默认接 Director.rawCompletion，测试可注入假实现）。
	complete func(worldID, task, prompt string) (string, error)
	cfgStore *config.Store // summary_chunk_size 热读取（可为 nil，用默认值）
	// memory 长期记忆存储：非 nil 时阶段摘要写入 world_memory（直接迁移，
	// 不再写 WorldState.Summaries）；nil 时回退旧字段（JSON 后端/测试）。
	memory *memory.Store

	mu          sync.Mutex
	summarizing map[string]bool // 防并发总结: worldID
	compacting  map[string]bool // 防并发篇章合并: worldID
}

// SetMemory 注入长期记忆存储（由 main.go 在装配阶段调用）。
func (s *SummaryService) SetMemory(store *memory.Store) { s.memory = store }

// NewSummaryService 创建阶段摘要服务。director 可为 nil（禁用自动摘要）。
func NewSummaryService(worldEngine *world.Engine, director *Director, cfgStore *config.Store) *SummaryService {
	s := &SummaryService{
		worldEngine: worldEngine,
		cfgStore:    cfgStore,
		summarizing: make(map[string]bool),
		compacting:  make(map[string]bool),
	}
	if director != nil {
		s.complete = director.rawCompletion
	}
	return s
}

// chunkSize 当前生效的摘要批次（热更新配置，缺省 DefaultSummaryChunk）。
func (s *SummaryService) chunkSize() int {
	if s.cfgStore != nil {
		if n := s.cfgStore.GetInt(config.KeySummaryChunk, DefaultSummaryChunk); n > 0 {
			return n
		}
	}
	return DefaultSummaryChunk
}

// MaybeSummarize 检查待总结队列，攒够批次则异步总结为一个阶段条目追加到 Summaries。
// 回合结束后由 TurnEngine 以 goroutine 调用；未攒够或正在总结时直接返回。
// 每次调用最多处理一个批次；积压在后续回合逐批消化（保持"一个阶段一条"的粒度）。
func (s *SummaryService) MaybeSummarize(worldID string) {
	if s.worldEngine == nil || s.complete == nil || worldID == "" {
		return
	}

	s.mu.Lock()
	if s.summarizing[worldID] {
		s.mu.Unlock()
		return
	}
	s.summarizing[worldID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.summarizing, worldID)
		s.mu.Unlock()
	}()

	chunk := s.chunkSize()
	if chunk < 1 {
		chunk = DefaultSummaryChunk
	}
	if chunk > world.MaxPendingSummaryTurns {
		chunk = world.MaxPendingSummaryTurns
	}

	// 锁内只读取不可变快照，外部 LLM 调用不阻塞世界回合。
	s.worldEngine.Lock(worldID)
	ws := s.worldEngine.LoadOrNil(worldID)
	if ws == nil || len(ws.PendingSummaryTurns) < chunk {
		s.worldEngine.Unlock(worldID)
		return
	}
	batch := append([]world.DialogueTurn(nil), ws.PendingSummaryTurns[:chunk]...)
	prev := s.previousSummaryText(worldID, ws)
	prompt := buildSummaryPrompt(ws, batch, prev)
	s.worldEngine.Unlock(worldID)

	ctx, cancel := context.WithTimeout(context.Background(), SummaryTimeout)
	newSummary, err := completeWithContext(ctx, s.complete, worldID, "summarizer", prompt)
	cancel()
	if err != nil {
		log.Printf("[Summary] 摘要生成失败（待总结队列保留，下轮重试）: world=%s: %v", worldID, err)
		return
	}
	newSummary = strings.TrimSpace(newSummary)
	if newSummary == "" {
		return
	}

	// 落库前只校验"待总结队列仍以本批次开头"：回合推进本身会改 Revision，
	// 若沿用整份状态 Revision 相等校验，高频会话里摘要会长期失败并每回合
	// 白烧一次 LLM 调用（旧实现的实际问题）。队列是 append-only 且只有本
	// 服务从头部消费，前缀匹配即可保证不会重复/错位消费。
	s.worldEngine.Lock(worldID)
	defer s.worldEngine.Unlock(worldID)
	latest := s.worldEngine.LoadOrNil(worldID)
	if latest == nil || !pendingPrefixMatches(latest.PendingSummaryTurns, batch) {
		log.Printf("[Summary] 待总结队列已变化，丢弃本次摘要结果（下一回合重试）: world=%s", worldID)
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if s.memory != nil {
		// 直接迁移：阶段摘要写入长期记忆（episode 块），不再写 WorldState.Summaries。
		// DedupeKey 只带轮次范围：写入成功但队列落库失败时，重试不会产生重复摘要。
		if _, err := s.memory.Append(memory.Chunk{
			WorldID:    worldID,
			Kind:       memory.KindEpisode,
			Level:      memory.LevelChunk,
			RoundFrom:  batch[0].Round,
			RoundTo:    batch[len(batch)-1].Round,
			Text:       newSummary,
			Importance: 7,
			DedupeKey:  fmt.Sprintf("episode|%d-%d", batch[0].Round, batch[len(batch)-1].Round),
			CreatedAt:  now,
		}); err != nil {
			log.Printf("[Summary] 摘要写入长期记忆失败（待总结队列保留）: world=%s: %v", worldID, err)
			return
		}
		latest.PendingSummaryTurns = latest.PendingSummaryTurns[chunk:]
		if err := s.worldEngine.SaveIfRevision(latest, latest.Revision); err != nil {
			log.Printf("[Summary] 待总结队列落库失败（摘要已写入记忆，重试按轮次去重）: world=%s: %v", worldID, err)
			return
		}
		log.Printf("[Summary] 阶段摘要已写入长期记忆: world=%s, 覆盖第%d-%d轮, %d 字",
			worldID, batch[0].Round, batch[len(batch)-1].Round, len([]rune(newSummary)))
		return
	}

	id := latest.AppendSummary(newSummary, batch[0].Round, batch[len(batch)-1].Round, now)
	latest.PendingSummaryTurns = latest.PendingSummaryTurns[chunk:]
	if len(latest.Summaries) > SummaryMaxEntries {
		dropped := len(latest.Summaries) - SummaryMaxEntries
		latest.Summaries = latest.Summaries[len(latest.Summaries)-SummaryMaxEntries:]
		// 早期摘要被裁掉即永久失忆，必须留痕（后续由层级摘要方案兜底）。
		log.Printf("[Summary] 摘要条数超上限，丢弃最早 %d 条（世界=%s，上限=%d）",
			dropped, worldID, SummaryMaxEntries)
	}
	if err := s.worldEngine.SaveIfRevision(latest, latest.Revision); err != nil {
		log.Printf("[Summary] 摘要落库失败（待总结队列保留）: world=%s: %v", worldID, err)
		return
	}
	log.Printf("[Summary] 阶段摘要已追加: world=%s, id=%d, 覆盖第%d-%d轮, %d 字",
		worldID, id, batch[0].Round, batch[len(batch)-1].Round, len([]rune(newSummary)))
}

// MaybeCompact 检查未归档的阶段摘要是否攒够一批，够则合并为一条"篇章摘要"
// （level=2，RAPTOR 式层级压缩）。由 TurnEngine 在回合结束后与摘要同批异步调用。
// 失败时子摘要保持未归档状态，下次重试（arc 按轮次范围去重）。
func (s *SummaryService) MaybeCompact(worldID string) {
	if s.memory == nil || s.complete == nil || worldID == "" {
		return
	}
	s.mu.Lock()
	if s.compacting[worldID] {
		s.mu.Unlock()
		return
	}
	s.compacting[worldID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.compacting, worldID)
		s.mu.Unlock()
	}()

	batch, err := s.memory.UncompactedEpisodes(worldID, DefaultArcWindow)
	if err != nil || len(batch) < DefaultArcWindow {
		return
	}
	batch = batch[:DefaultArcWindow]

	ctx, cancel := context.WithTimeout(context.Background(), SummaryTimeout)
	arcText, err := completeWithContext(ctx, s.complete, worldID, "archivist", buildArcPrompt(batch))
	cancel()
	if err != nil {
		log.Printf("[Summary] 篇章摘要生成失败（子摘要保持未归档，下轮重试）: world=%s: %v", worldID, err)
		return
	}
	arcText = strings.TrimSpace(arcText)
	if arcText == "" {
		return
	}

	children := make([]int64, 0, len(batch))
	for _, c := range batch {
		children = append(children, c.ID)
	}
	from, to := batch[0].RoundFrom, batch[len(batch)-1].RoundTo
	id, err := s.memory.InsertArc(memory.Chunk{
		WorldID:    worldID,
		Kind:       memory.KindEpisode,
		RoundFrom:  from,
		RoundTo:    to,
		Text:       arcText,
		Importance: 8,
		DedupeKey:  fmt.Sprintf("arc|%d-%d", from, to),
		CreatedAt:  time.Now().Format("2006-01-02 15:04:05"),
	}, children)
	if err != nil {
		log.Printf("[Summary] 篇章摘要落库失败（子摘要保持未归档）: world=%s: %v", worldID, err)
		return
	}
	log.Printf("[Summary] 篇章摘要已合并: world=%s, arc=%d, 覆盖第%d-%d轮（%d 条阶段摘要）",
		worldID, id, from, to, len(batch))
}

// buildArcPrompt 把一批阶段摘要合并为一条篇章摘要。
func buildArcPrompt(batch []memory.Chunk) string {
	var sb strings.Builder
	sb.WriteString("你在为一个角色扮演故事做长篇记忆压缩。下面是一组连续的「阶段摘要」。\n")
	sb.WriteString("请把它们合并成一段「篇章摘要」：\n")
	sb.WriteString("- 第三人称连贯叙述，突出主线推进、关键人物关系变化、重要物品/线索、仍未解决的悬念\n")
	sb.WriteString("- 丢弃寒暄与重复描写；不要逐条罗列，也不要编造摘要中没有的事\n")
	sb.WriteString("- 总长不超过 300 字；只输出摘要正文，不要标题或解释\n\n")
	sb.WriteString("【阶段摘要】\n")
	for _, c := range batch {
		if c.RoundTo > 0 {
			fmt.Fprintf(&sb, "- [第%d-%d轮] %s\n", c.RoundFrom, c.RoundTo, c.Text)
		} else {
			fmt.Fprintf(&sb, "- %s\n", c.Text)
		}
	}
	return sb.String()
}

func completeWithContext(ctx context.Context, complete func(string, string, string) (string, error), worldID, task, prompt string) (string, error) {
	result := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, err := complete(worldID, task, prompt)
		result <- struct {
			text string
			err  error
		}{text, err}
	}()
	select {
	case out := <-result:
		return out.text, out.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// pendingPrefixMatches 判断待总结队列是否仍以给定批次开头
// （Round + 双方文本逐条比对；队列 append-only 且只有摘服务从头部消费）。
func pendingPrefixMatches(pending, batch []world.DialogueTurn) bool {
	if len(pending) < len(batch) {
		return false
	}
	for i := range batch {
		if pending[i].Round != batch[i].Round ||
			pending[i].Player != batch[i].Player ||
			pending[i].Narrator != batch[i].Narrator {
			return false
		}
	}
	return true
}

// previousSummaryText 返回最近一条剧情摘要（篇章/阶段，长期记忆优先，旧字段回退）。
func (s *SummaryService) previousSummaryText(worldID string, ws *world.WorldState) string {
	if s.memory != nil {
		if items, err := s.memory.Timeline(worldID, 1, 1); err == nil && len(items) > 0 {
			return items[len(items)-1].Text
		}
		return ""
	}
	if entries := ws.SummaryEntries(); len(entries) > 0 {
		return entries[len(entries)-1].Text
	}
	return ""
}

// buildSummaryPrompt 组装阶段摘要 prompt：上一条摘要（仅供衔接，不复述）+ 主线/目标现状
// + 本阶段对话 → 一段新摘要。
//
// 主线/目标的现状单独给出，是为了让摘要链在"幕推进、目标完成、悬念新增"这类
// 长期线索上不失真——只喂对话时，跨段的主线信息会随摘要链逐渐丢失。
func buildSummaryPrompt(ws *world.WorldState, batch []world.DialogueTurn, prev string) string {
	var sb strings.Builder
	sb.WriteString("你在为一个角色扮演故事写阶段性剧情摘要。下面给你「上一阶段的摘要」（仅供你衔接上下文，不要复述它）和「本阶段的对话记录」。\n")
	sb.WriteString("请只为本阶段对话写一段摘要。要求：\n")
	sb.WriteString("- 第三人称连贯叙述，保留关键决定、人物关系变化、获得的物品/线索、未解决的悬念\n")
	sb.WriteString("- 若本阶段推进或改变了主线、目标、悬念状态，请明确写出变化后的状态\n")
	sb.WriteString("- 省略寒暄、重复描写和无关细节；总长不超过 250 字\n")
	sb.WriteString("- 只输出摘要正文，不要标题、列表或任何解释\n\n")
	if strings.TrimSpace(prev) != "" {
		sb.WriteString("【上一阶段摘要】\n" + prev + "\n\n")
	} else {
		sb.WriteString("【上一阶段摘要】\n（暂无，这是故事的开头）\n\n")
	}
	if ctx := buildSummaryStateHint(ws); ctx != "" {
		sb.WriteString("【当前主线与目标】\n" + ctx + "\n")
	}
	sb.WriteString("【本阶段对话记录】\n")
	for _, turn := range batch {
		fmt.Fprintf(&sb, "玩家: %s\n世界: %s\n", turn.Player, turn.Narrator)
	}
	return sb.String()
}

// buildSummaryStateHint 生成摘要用的主线/目标现状提示（只列当前事实，不列历史）。
func buildSummaryStateHint(ws *world.WorldState) string {
	var sb strings.Builder
	if sl := ws.Storyline; sl != nil && strings.TrimSpace(sl.Title) != "" {
		sb.WriteString("主线: " + sl.Title + "\n")
		for _, a := range sl.Acts {
			if a.Status == "active" {
				sb.WriteString("进行中的幕: " + a.Title + "\n")
				break
			}
		}
	}
	pending := make([]string, 0, 3)
	for _, q := range ws.Quests {
		if !q.Completed && strings.TrimSpace(q.Description) != "" {
			pending = append(pending, q.Description)
			if len(pending) >= 3 {
				break
			}
		}
	}
	if len(pending) > 0 {
		sb.WriteString("未完成目标: " + strings.Join(pending, "；") + "\n")
	}
	return sb.String()
}
