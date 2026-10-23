package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func TestContextBuilderBudgetUsesUnicodeAndRemainingTailBudget(t *testing.T) {
	ws := world.NewWorldState("budget", world.ModeSimRPG)
	ws.ReplyStyle = strings.Repeat("风", 30)
	turns := []world.DialogueTurn{{Player: strings.Repeat("行", 20), Narrator: strings.Repeat("答", 20)}}
	lore := &world.LoreResult{Tail: []world.LoreHit{{Entry: world.LoreEntry{Title: "可选", Content: strings.Repeat("尾", 30)}}}}
	input := NewContextBuilder(50).BuildNarratorInputWithBudget(50, ws, lore, nil, "", "", turns, "当前", "长度要求", "", "")
	if input.PlayerMessage != "当前" || !strings.Contains(input.Tail, "长度要求") {
		t.Fatalf("必需输入不应丢失: %+v", input)
	}
	if strings.Contains(input.Tail, strings.Repeat("尾", 30)) {
		t.Fatal("尾部可选 lore 不应突破剩余预算")
	}
}
