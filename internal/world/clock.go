// 世界时钟与离线演化（设计文档 4.2 / 6.3）。
//
// 时间模型：WorldTime 为世界内分钟计数，所有时间相关计算
// （情绪衰减、记忆 recency、定时事件）均使用世界时间。
// 离线演化采用"回归结算"（fastForward）：玩家回归时按离线时长
// 一次性结算到期事件与衰减，而非后台常驻模拟（成本约束）。
package world

import (
	"fmt"
	"time"
)

// 默认回合时间推进：一次玩家交互约推进 10 分钟世界时间。
const DefaultRoundMinutes = 10

// realToWorldRatio 现实分钟到世界分钟的映射（1:1，可按世界观调整）。
const realToWorldRatio = 1.0

// offlineThresholdMinutes 触发离线演化提示的最小现实离线时长。
const offlineThresholdMinutes = 360 // 6 小时

// AdvanceClock 推进世界时钟，返回到期的定时事件。
func AdvanceClock(ws *WorldState, minutes int64) []ScheduledEvent {
	if minutes <= 0 {
		return nil
	}
	ws.Clock.WorldTime += minutes

	var due []ScheduledEvent
	for i := range ws.EventQueue {
		ev := &ws.EventQueue[i]
		if ev.TriggerAt > 0 && ev.TriggerAt <= ws.Clock.WorldTime && !ev.Triggered {
			ev.Triggered = true
			due = append(due, *ev)
		}
	}
	return due
}

// FastForwardResult 离线演化结算结果。
type FastForwardResult struct {
	ElapsedWorldMinutes int64    // 离线折算的世界分钟数
	FiredEvents         []string // 到期触发的事件描述
}

// FastForward 离线演化：玩家回归时按现实离线时长结算世界变迁。
// 规则化结算（零 LLM）：时钟推进、到期事件触发、情绪衰减。
// "期间发生了什么"的叙事摘要由调用方（TurnEngine）决定是否用 LLM 生成。
func FastForward(ws *WorldState, realNow time.Time) *FastForwardResult {
	if ws.Clock.RealLapsed <= 0 {
		ws.Clock.RealLapsed = realNow.Unix()
		return nil
	}

	elapsedReal := realNow.Unix() - ws.Clock.RealLapsed
	ws.Clock.RealLapsed = realNow.Unix()
	elapsedMinutes := elapsedReal / 60
	if elapsedMinutes < offlineThresholdMinutes {
		return nil
	}

	worldMinutes := int64(float64(elapsedMinutes) * realToWorldRatio)
	due := AdvanceClock(ws, worldMinutes)

	// 注：情绪衰减由回合记账统一执行（DecayMoods 按世界时钟差值计算），
	// 时钟推进后自然覆盖离线时段，此处不重复衰减。

	result := &FastForwardResult{
		ElapsedWorldMinutes: worldMinutes,
	}
	for _, ev := range due {
		result.FiredEvents = append(result.FiredEvents, fmt.Sprintf("%s（%s）", ev.Description, ev.Type))
	}
	return result
}

// TouchClock 更新现实时间锚点（每回合调用）。
func TouchClock(ws *WorldState, realNow time.Time) {
	ws.Clock.RealLapsed = realNow.Unix()
}

// ScheduleEvent 注册一个定时/条件事件。
func (ws *WorldState) ScheduleEvent(ev ScheduledEvent) {
	for i := range ws.EventQueue {
		if ws.EventQueue[i].ID == ev.ID {
			ws.EventQueue[i] = ev
			return
		}
	}
	ws.EventQueue = append(ws.EventQueue, ev)
}
