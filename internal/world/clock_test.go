package world

import (
	"testing"
	"time"
)

func TestAdvanceClock_DueEvents(t *testing.T) {
	ws := NewWorldState("w1", ModeTRPG)
	ws.EventQueue = []ScheduledEvent{
		{ID: "e1", Description: "商队抵达", TriggerAt: 100, Type: "world"},
		{ID: "e2", Description: "季节变化", TriggerAt: 500, Type: "world"},
		{ID: "e3", Description: "条件触发事件", Type: "trigger"}, // 无 TriggerAt，不走时钟
	}

	due := AdvanceClock(ws, 120)
	if len(due) != 1 || due[0].ID != "e1" {
		t.Fatalf("应只有 e1 到期: %+v", due)
	}
	if ws.Clock.WorldTime != 120 {
		t.Fatalf("时钟应推进到 120: %d", ws.Clock.WorldTime)
	}
	if !ws.EventQueue[0].Triggered {
		t.Fatal("e1 应被标记为已触发")
	}
	if ws.EventQueue[2].Triggered {
		t.Fatal("条件事件不应被时钟触发")
	}

	// 再次推进到 e2 到期
	due = AdvanceClock(ws, 400)
	if len(due) != 1 || due[0].ID != "e2" {
		t.Fatalf("应只有 e2 到期: %+v", due)
	}
}

func TestFastForward_BelowThreshold(t *testing.T) {
	ws := NewWorldState("w1", ModeSimRPG)
	now := time.Now()
	ws.Clock.RealLapsed = now.Add(-1 * time.Hour).Unix() // 离线 1 小时 < 6 小时阈值

	if ff := FastForward(ws, now); ff != nil {
		t.Fatalf("低于阈值不应触发离线演化: %+v", ff)
	}
}

func TestFastForward_FiresDueEvents(t *testing.T) {
	ws := NewWorldState("w1", ModeSimRPG)
	now := time.Now()
	ws.Clock.RealLapsed = now.Add(-24 * time.Hour).Unix() // 离线 24 小时
	ws.EventQueue = []ScheduledEvent{
		{ID: "e1", Description: "商队抵达", TriggerAt: 600, Type: "world"}, // 离线 10 小时后到期
	}

	ff := FastForward(ws, now)
	if ff == nil {
		t.Fatal("超过阈值应触发离线演化")
	}
	if ff.ElapsedWorldMinutes != 1440 {
		t.Fatalf("离线应折算为 1440 世界分钟: %d", ff.ElapsedWorldMinutes)
	}
	if len(ff.FiredEvents) != 1 {
		t.Fatalf("应有 1 个事件到期: %+v", ff.FiredEvents)
	}
	if ws.Clock.RealLapsed != now.Unix() {
		t.Fatal("RealLapsed 应更新为当前时间")
	}
}

func TestDecayMoods(t *testing.T) {
	ws := NewWorldState("w1", ModeTRPG)
	ws.Characters["甲"] = &CharacterState{
		Name: "甲", Kind: "npc", Alive: true,
		Mood: MoodState{Valence: 70, Arousal: 70, Tags: []string{"感激"}, UpdatedAt: 0},
	}
	ws.Characters["乙"] = &CharacterState{
		Name: "乙", Kind: "npc", Alive: true, Traits: []string{"记仇"},
		Mood: MoodState{Valence: -70, Arousal: 70, Tags: []string{"不满"}, UpdatedAt: 0},
	}

	// 推进一世界日
	ws.Clock.WorldTime = minutesPerWorldDay
	DecayMoods(ws)

	// 甲：0.7 衰减 → 49
	if got := ws.Characters["甲"].Mood.Valence; got != 49 {
		t.Fatalf("甲 valence 应为 49: %d", got)
	}
	// 乙：记仇，负面情绪衰减减半（factor^0.5 ≈ 0.8366）→ -70*0.8366 ≈ -58
	if got := ws.Characters["乙"].Mood.Valence; got >= -49 {
		t.Fatalf("记仇 NPC 负面情绪应衰减更慢: %d", got)
	}

	// 推进 10 世界日，情绪应基本平复并清除标签
	ws.Clock.WorldTime = 10 * minutesPerWorldDay
	DecayMoods(ws)
	if v := ws.Characters["甲"].Mood.Valence; abs(v) >= 10 {
		t.Fatalf("10 天后 valence 应趋近平复: %d", v)
	}
	if len(ws.Characters["甲"].Mood.Tags) != 0 {
		t.Fatal("平复后情绪标签应被清除")
	}
}

func TestRetrieve_ImportanceAndRecency(t *testing.T) {
	now := int64(10000)
	entries := []MemoryEntry{
		{ID: "m1", Content: "玩家买了一杯茶", Importance: 2, WorldTime: now, LastAccess: now},
		{ID: "m2", Content: "玩家帮助老陈修好了屋顶", Importance: 8, WorldTime: now - 6000, LastAccess: now - 6000},
		{ID: "m3", Content: "玩家随手关了一扇门", Importance: 3, WorldTime: now, LastAccess: now},
		{ID: "m4", Content: "已作废的记忆", Importance: 10, Invalid: true, WorldTime: now, LastAccess: now},
	}

	top := Retrieve(entries, "老陈 屋顶", now, 3, nil)
	if len(top) == 0 {
		t.Fatal("应检索到记忆")
	}
	// 失效记忆不应出现
	for _, e := range top {
		if e.ID == "m4" {
			t.Fatal("失效记忆不应被检索到")
		}
	}
	// 高重要性 + 高相关的 m2 应排第一
	if top[0].ID != "m2" {
		t.Fatalf("m2 应排第一: %+v", top[0])
	}
	// LastAccess 应被更新（保鲜）
	if entries[1].LastAccess != now {
		t.Fatal("被检索记忆的 LastAccess 应更新")
	}
}

func TestRetrieve_PinnedNeverDropped(t *testing.T) {
	now := int64(100000)
	entries := []MemoryEntry{
		{ID: "m1", Content: "重大事件", Importance: 9, Pinned: true, WorldTime: 0, LastAccess: 0},
		{ID: "m2", Content: "普通小事", Importance: 1, WorldTime: now, LastAccess: now},
	}
	top := Retrieve(entries, "", now, 2, nil)
	found := false
	for _, e := range top {
		if e.ID == "m1" {
			found = true
		}
	}
	if !found {
		t.Fatal("Pinned 高重要性记忆应始终可被检索")
	}
}

func TestBigramOverlap(t *testing.T) {
	if got := bigramOverlap("老陈的商店", "老陈在商店里算账"); got <= 0 {
		t.Fatalf("应有重叠: %f", got)
	}
	if got := bigramOverlap("完全无关", "老陈在商店里算账"); got != 0 {
		t.Fatalf("应无重叠: %f", got)
	}
}

func TestGetMode(t *testing.T) {
	m := GetMode(ModeSimRPG)
	if !m.EnableOffline {
		t.Fatal("simrpg 应启用离线演化")
	}
	m = GetMode(ModeRoleplay)
	if m.EnableRules || m.EnableScript {
		t.Fatal("roleplay 应关闭规则层与剧本")
	}
	m = GetMode("unknown")
	if m.Name != ModeTRPG {
		t.Fatal("未知模式应回退为 trpg")
	}
}
