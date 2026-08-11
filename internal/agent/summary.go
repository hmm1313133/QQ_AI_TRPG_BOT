// Package agent - SummaryService 阶段性剧情摘要服务。
//
// 记忆 = 上下文总结（SillyTavern 式摘要链）：RecentTurns 固定窗口承载近期
// 逐字对话，滑出窗口的对话轮积累到 WorldState.PendingSummaryTurns，攒够
// summary_chunk_size 轮后由本服务异步总结为「一条」阶段摘要追加到
// WorldState.Summaries——一个阶段一条，不再滚动合并成单条。
// 注入上下文时多条阶段摘要按时间顺序全量列出，替代远期对话窗口。
// 具体数值/剧情线由世界引擎（EventLog / ScenePlan / Metrics）承载，不进摘要。
package agent

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultSummaryChunk 默认待总结批次（轮数）：攒够这么多淘汰轮才触发一次摘要。
const DefaultSummaryChunk = 6

// SummaryService 阶段性剧情摘要服务。
type SummaryService struct {
	worldEngine *world.Engine
	// complete 低温度纯文本补全（默认接 Director.rawCompletion，测试可注入假实现）。
	complete func(worldID, task, prompt string) (string, error)
	cfgStore *config.Store // summary_chunk_size 热读取（可为 nil，用默认值）

	mu          sync.Mutex
	summarizing map[string]bool // 防并发总结: worldID
}

// NewSummaryService 创建阶段摘要服务。director 可为 nil（禁用自动摘要）。
func NewSummaryService(worldEngine *world.Engine, director *Director, cfgStore *config.Store) *SummaryService {
	s := &SummaryService{
		worldEngine: worldEngine,
		cfgStore:    cfgStore,
		summarizing: make(map[string]bool),
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

	s.worldEngine.Lock(worldID)
	defer s.worldEngine.Unlock(worldID)

	ws := s.worldEngine.LoadOrNil(worldID)
	if ws == nil || len(ws.PendingSummaryTurns) < s.chunkSize() {
		return
	}

	// 取最早的一个批次总结为一个阶段条目，其余留待后续回合
	batch := ws.PendingSummaryTurns[:s.chunkSize()]
	newSummary, err := s.complete(worldID, "summarizer", buildSummaryPrompt(ws, batch))
	if err != nil {
		log.Printf("[Summary] 摘要生成失败（待总结队列保留，下轮重试）: world=%s: %v", worldID, err)
		return
	}
	newSummary = strings.TrimSpace(newSummary)
	if newSummary == "" {
		log.Printf("[Summary] 摘要为空（待总结队列保留）: world=%s", worldID)
		return
	}

	id := ws.AppendSummary(newSummary, batch[0].Round, batch[len(batch)-1].Round,
		time.Now().Format("2006-01-02 15:04:05"))
	ws.PendingSummaryTurns = ws.PendingSummaryTurns[len(batch):]
	if err := s.worldEngine.Save(ws); err != nil {
		log.Printf("[Summary] 摘要落库失败: world=%s: %v", worldID, err)
		return
	}
	log.Printf("[Summary] 阶段摘要已追加: world=%s, id=%d, 覆盖第%d-%d轮, %d 字",
		worldID, id, batch[0].Round, batch[len(batch)-1].Round, len([]rune(newSummary)))
}

// buildSummaryPrompt 组装阶段摘要 prompt：上一条摘要（仅供衔接，不复述）+ 本阶段对话 → 一段新摘要。
func buildSummaryPrompt(ws *world.WorldState, batch []world.DialogueTurn) string {
	var sb strings.Builder
	sb.WriteString("你在为一个角色扮演故事写阶段性剧情摘要。下面给你「上一阶段的摘要」（仅供你衔接上下文，不要复述它）和「本阶段的对话记录」。\n")
	sb.WriteString("请只为本阶段对话写一段摘要。要求：\n")
	sb.WriteString("- 第三人称连贯叙述，保留关键决定、人物关系变化、获得的物品/线索、未解决的悬念\n")
	sb.WriteString("- 省略寒暄、重复描写和无关细节；总长不超过 250 字\n")
	sb.WriteString("- 只输出摘要正文，不要标题、列表或任何解释\n\n")
	if entries := ws.SummaryEntries(); len(entries) > 0 {
		sb.WriteString("【上一阶段摘要】\n" + entries[len(entries)-1].Text + "\n\n")
	} else {
		sb.WriteString("【上一阶段摘要】\n（暂无，这是故事的开头）\n\n")
	}
	sb.WriteString("【本阶段对话记录】\n")
	for _, turn := range batch {
		fmt.Fprintf(&sb, "玩家: %s\n世界: %s\n", turn.Player, turn.Narrator)
	}
	return sb.String()
}
