package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func TestAIGuidanceReplyToRuleGuidance(t *testing.T) {
	r := &aiGuidanceReply{
		Tone:        " 紧张 ",
		Pacing:      "fast",
		Focus:       []string{" 推进调查 ", "", "  "},
		Constraints: []string{"不要剧透"},
	}
	g := r.toRuleGuidance()
	if g.Tone != "紧张" {
		t.Errorf("Tone 应去空白，got %q", g.Tone)
	}
	if g.Pacing != "fast" {
		t.Errorf("Pacing 应保留 fast，got %q", g.Pacing)
	}
	if len(g.Focus) != 1 || g.Focus[0] != "推进调查" {
		t.Errorf("Focus 应过滤空白条目，got %v", g.Focus)
	}
	if len(g.Constraints) != 1 {
		t.Errorf("Constraints 长度应为 1，got %v", g.Constraints)
	}
}

func TestAIGuidanceReplyDefaults(t *testing.T) {
	g := (&aiGuidanceReply{}).toRuleGuidance()
	if g.Tone != "正常" {
		t.Errorf("空 Tone 应回退“正常”，got %q", g.Tone)
	}
	if g.Pacing != "medium" {
		t.Errorf("空 Pacing 应回退 medium，got %q", g.Pacing)
	}

	g2 := (&aiGuidanceReply{Pacing: "hyper"}).toRuleGuidance()
	if g2.Pacing != "medium" {
		t.Errorf("非法 Pacing 应回退 medium，got %q", g2.Pacing)
	}
}

func TestSanitizeGuidanceUpdates(t *testing.T) {
	updates := []StateUpdate{
		{Type: "npc_disposition", Target: "老板娘", Value: "hostile"},
		{Type: "destroy_world", Target: "x", Value: "y"},     // 非法类型
		{Type: "hidden_discovered", Target: "  ", Value: ""}, // 空 target
		{Type: "objective_completed", Target: "找到钥匙", Value: "done"},
	}
	out := sanitizeGuidanceUpdates(updates)
	if len(out) != 2 {
		t.Fatalf("应保留 2 条合法更新，got %d", len(out))
	}
	if out[0].Target != "老板娘" || out[1].Target != "找到钥匙" {
		t.Errorf("保留的更新不符合预期: %+v", out)
	}
}

func TestSanitizeGuidanceUpdatesCap(t *testing.T) {
	updates := make([]StateUpdate, 0, guidanceMaxUpdates+3)
	for i := 0; i < guidanceMaxUpdates+3; i++ {
		updates = append(updates, StateUpdate{Type: "event_triggered", Target: "事件", Value: "x"})
	}
	if got := len(sanitizeGuidanceUpdates(updates)); got != guidanceMaxUpdates {
		t.Errorf("更新条数应被截断到 %d，got %d", guidanceMaxUpdates, got)
	}
}

func TestBuildGuidanceUserMessage(t *testing.T) {
	state := &world.WorldState{
		RoundCount: 7,
		Scene: world.SceneState{
			NodeName: "酒馆", NodeType: "社交", DangerLevel: "安全",
			Description: "喧闹的酒馆",
		},
		Metrics: world.Metrics{TensionLevel: 10, ChaosLevel: 5, PlayerAgency: 60, ObjectiveProgress: 0},
		Characters: map[string]*world.CharacterState{
			"老板娘": {Name: "老板娘", Disposition: "neutral", Alive: true},
		},
		Quests: []world.QuestState{
			{Description: "打听消息", Completed: false},
			{Description: "进入酒馆", Completed: true},
		},
		EventQueue: []world.ScheduledEvent{
			{Description: "醉汉闹事"},
			{Description: "已触发事件", Triggered: true},
		},
		HiddenInfo: []world.HiddenItem{
			{Description: "地窖的秘密"},
			{Description: "已发现线索", Discovered: true},
		},
		ScenePlan: "叙事重点: 打听",
		RecentTurns: []world.DialogueTurn{
			{Player: "我坐下", Narrator: "你坐下了"},
		},
	}

	msg := buildGuidanceUserMessage(state, "我叫住老板娘")

	for _, want := range []string{
		"酒馆", "安全", "张力 10", "老板娘", "打听消息", "[x] 进入酒馆",
		"醉汉闹事", "地窖的秘密", "场景计划", "我坐下", "我叫住老板娘",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("用户消息应包含 %q", want)
		}
	}
	// 已触发事件与已发现线索不应再出现
	if strings.Contains(msg, "已触发事件") {
		t.Error("已触发事件不应出现在待触发列表")
	}
	if strings.Contains(msg, "已发现线索") {
		t.Error("已发现线索不应出现在未发现列表")
	}
}
