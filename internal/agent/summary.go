// Package agent - SummaryService 滚动剧情摘要服务。
//
// 记忆 = 上下文总结（SillyTavern 式滚动摘要）：RecentTurns 固定窗口承载近期
// 逐字对话，滑出窗口的对话轮积累到 WorldState.PendingSummaryTurns，攒够
// summary_chunk_size 轮后由本服务异步调用 LLM 合并进 CampaignSummary。
// 具体数值/剧情线由世界引擎（EventLog / ScenePlan / Metrics）承载，不进摘要。
package agent

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultSummaryChunk 默认待总结批次（轮数）：攒够这么多淘汰轮才触发一次摘要。
const DefaultSummaryChunk = 6

// SummaryService 滚动剧情摘要服务。
type SummaryService struct {
	worldEngine *world.Engine
	// complete 低温度纯文本补全（默认接 Director.rawCompletion，测试可注入假实现）。
	complete func(worldID, task, prompt string) (string, error)
	cfgStore *config.Store // summary_chunk_size 热读取（可为 nil，用默认值）

	mu          sync.Mutex
	summarizing map[string]bool // 防并发总结: worldID
}

// NewSummaryService 创建滚动摘要服务。director 可为 nil（禁用自动摘要）。
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

// MaybeSummarize 检查待总结队列，攒够批次则异步合并进 CampaignSummary。
// 回合结束后由 TurnEngine 以 goroutine 调用；未攒够或正在总结时直接返回。
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

	newSummary, err := s.complete(worldID, "summarizer", buildSummaryPrompt(ws))
	if err != nil {
		log.Printf("[Summary] 摘要生成失败（待总结队列保留，下轮重试）: world=%s: %v", worldID, err)
		return
	}
	newSummary = strings.TrimSpace(newSummary)
	if newSummary == "" {
		log.Printf("[Summary] 摘要为空（待总结队列保留）: world=%s", worldID)
		return
	}

	ws.CampaignSummary = newSummary
	ws.PendingSummaryTurns = nil
	if err := s.worldEngine.Save(ws); err != nil {
		log.Printf("[Summary] 摘要落库失败: world=%s: %v", worldID, err)
		return
	}
	log.Printf("[Summary] 剧情摘要已滚动更新: world=%s, %d 字", worldID, len([]rune(newSummary)))
}

// buildSummaryPrompt 组装滚动摘要 prompt：已有摘要 + 待总结对话 → 合并后的一段摘要。
func buildSummaryPrompt(ws *world.WorldState) string {
	var sb strings.Builder
	sb.WriteString("你在维护一个故事世界的故事梗概。下面是到目前为止的剧情摘要，以及最近发生的一段对话。\n")
	sb.WriteString("请把新对话合并进剧情摘要，输出一段更新后的完整摘要。要求：\n")
	sb.WriteString("- 第三人称连贯叙述，保留关键决定、人物关系变化、获得的物品/线索、未解决的悬念\n")
	sb.WriteString("- 省略寒暄、重复描写和无关细节；总长不超过 400 字\n")
	sb.WriteString("- 只输出摘要正文，不要标题、列表或任何解释\n\n")
	if strings.TrimSpace(ws.CampaignSummary) != "" {
		sb.WriteString("【已有剧情摘要】\n" + ws.CampaignSummary + "\n\n")
	} else {
		sb.WriteString("【已有剧情摘要】\n（暂无，这是第一次总结）\n\n")
	}
	sb.WriteString("【待合并的最近对话】\n")
	for _, turn := range ws.PendingSummaryTurns {
		fmt.Fprintf(&sb, "玩家: %s\n世界: %s\n", turn.Player, turn.Narrator)
	}
	return sb.String()
}
