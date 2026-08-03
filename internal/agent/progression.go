// Package agent - ProgressionEngine 成长引擎（设计文档 7.2）。
//
// 成长闭环：检定成功 → 记录 SkillUse（异步侧链，零 LLM）
//   → 幕间结算（场景切换触发）→ 角色卡更新 → 成长报告。
// 数值结算复用 trpg.Service 已有的 SkillGrowth（CoC7 规则）。
package agent

import (
	"log"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// ProgressionEngine 成长引擎。
type ProgressionEngine struct {
	svc         *trpg.Service
	worldEngine *world.Engine
}

// NewProgressionEngine 创建成长引擎。
func NewProgressionEngine(svc *trpg.Service, worldEngine *world.Engine) *ProgressionEngine {
	return &ProgressionEngine{svc: svc, worldEngine: worldEngine}
}

// RecordSkillUse 记录一次技能检定结果（仅成功时记录，供幕间成长结算）。
// 按 userID+skill 去重（同一幕内多次成功只结算一次成长）。
func (p *ProgressionEngine) RecordSkillUse(sessionID, userID, skill string, success bool) {
	if p.worldEngine == nil || !success || userID == "" || skill == "" {
		return
	}

	state := p.worldEngine.LoadOrNil(sessionID)
	if state == nil {
		return
	}

	for _, r := range state.SkillUses {
		if r.UserID == userID && r.Skill == skill {
			return // 已记录
		}
	}
	state.SkillUses = append(state.SkillUses, world.SkillUseRecord{
		UserID: userID,
		Skill:  skill,
		Round:  state.RoundCount,
	})

	if err := p.worldEngine.Save(state); err != nil {
		log.Printf("[Progression] 记录技能使用失败: %v", err)
	}
}

// PendingCount 返回待结算的技能使用记录数。
func (p *ProgressionEngine) PendingCount(sessionID string) int {
	if p.worldEngine == nil {
		return 0
	}
	state := p.worldEngine.LoadOrNil(sessionID)
	if state == nil {
		return 0
	}
	return len(state.SkillUses)
}

// Settle 幕间成长结算：对记录的技能逐个执行成长检定（CoC7），
// 清空记录并返回成长报告文本。无待结算记录时返回空串。
func (p *ProgressionEngine) Settle(sessionID string) string {
	if p.worldEngine == nil || p.svc == nil {
		return ""
	}

	state := p.worldEngine.LoadOrNil(sessionID)
	if state == nil || len(state.SkillUses) == 0 {
		return ""
	}

	var results []string
	for _, r := range state.SkillUses {
		res, err := p.svc.SkillGrowth(sessionID, r.UserID, r.Skill)
		if err != nil {
			log.Printf("[Progression] 成长检定失败: user=%s skill=%s: %v", r.UserID, r.Skill, err)
			continue
		}
		if res != nil && res.Detail != "" {
			results = append(results, res.Detail)
		}
	}

	// 清空待结算记录
	state.SkillUses = nil
	if err := p.worldEngine.Save(state); err != nil {
		log.Printf("[Progression] 清空技能记录失败: %v", err)
	}

	if len(results) == 0 {
		return ""
	}

	report := "【幕间成长结算】\n" + strings.Join(results, "\n")
	log.Printf("[Progression] 幕间结算: session=%s, %d 项", sessionID, len(results))
	return report
}
