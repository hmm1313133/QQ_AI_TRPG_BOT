// 主提示词预设解析测试。
package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// presetContentResolved 返回预设正文经 {{player}} 替换后的形态（与 ResolveMainPrompt 内部一致）。
func presetContentResolved(content, playerName string) string {
	if playerName == "" {
		playerName = "玩家"
	}
	return strings.ReplaceAll(content, playerPlaceholder, playerName)
}

func TestResolveMainPrompt_Default(t *testing.T) {
	got := ResolveMainPrompt(nil, "")
	if !strings.Contains(got, presetContentResolved(BuiltinPresets[0].Content, "")) {
		t.Fatal("nil style 应回退 default 预设")
	}
	if !strings.HasSuffix(got, narratorTechnicalContract) {
		t.Fatal("应自动追加技术契约段")
	}
	if strings.Contains(got, playerPlaceholder) {
		t.Fatal("占位符应被替换")
	}
}

func TestResolveMainPrompt_PresetByKey(t *testing.T) {
	style := &world.StyleConfig{PromptPreset: "novel"}
	got := ResolveMainPrompt(style, "")
	if !strings.Contains(got, presetContentResolved(presetByKey("novel").Content, "")) {
		t.Fatal("应按 key 取 novel 预设")
	}

	// 未知 key 回退 default
	style.PromptPreset = "no-such-preset"
	if got := ResolveMainPrompt(style, ""); !strings.Contains(got, presetContentResolved(BuiltinPresets[0].Content, "")) {
		t.Fatal("未知 preset key 应回退 default 预设")
	}
}

func TestResolveMainPrompt_CustomOverridesPreset(t *testing.T) {
	style := &world.StyleConfig{PromptPreset: "novel", CustomPrompt: "  我的自定义提示词 {{player}}  "}
	got := ResolveMainPrompt(style, "")
	if !strings.Contains(got, "我的自定义提示词") {
		t.Fatal("自定义提示词应覆盖预设")
	}
	if strings.Contains(got, presetByKey("novel").Content) {
		t.Fatal("预设内容不应出现在自定义覆盖结果中")
	}
	if !strings.HasSuffix(got, narratorTechnicalContract) {
		t.Fatal("自定义提示词也应自动追加技术契约段")
	}
}

func TestResolveMainPrompt_PlayerPlaceholder(t *testing.T) {
	style := &world.StyleConfig{CustomPrompt: "你在为 {{player}} 讲故事。"}
	if got := ResolveMainPrompt(style, "林月"); !strings.Contains(got, "你在为 林月 讲故事。") {
		t.Fatalf("{{player}} 应替换为人设名: %s", got)
	}
	if got := ResolveMainPrompt(style, ""); !strings.Contains(got, "你在为 玩家 讲故事。") {
		t.Fatalf("无人设名应回退\"玩家\": %s", got)
	}
}

func TestBuiltinPresets_NoTRPGWording(t *testing.T) {
	for _, p := range BuiltinPresets {
		if p.Key == "" || p.Name == "" || p.Content == "" {
			t.Fatalf("预设字段不完整: %+v", p)
		}
		for _, bad := range []string{"跑团", "KP", "TRPG", "COC", "DND", "DnD", "主持人"} {
			if strings.Contains(p.Content, bad) {
				t.Fatalf("预设 %s 含跑团残留措辞 %q", p.Key, bad)
			}
		}
	}
}
