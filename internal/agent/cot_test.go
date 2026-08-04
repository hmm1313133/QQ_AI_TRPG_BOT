package agent

import "testing"

// StripThinking：成对标签剥离并 TrimSpace；未闭合/无标签原样返回。
func TestStripThinking(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"成对标签剥离", "<think>①检查判断 ②核心推演</think>正文叙事", "正文叙事"},
		{"剥离后 TrimSpace", "  <think>思考</think>\n\n正文  ", "正文"},
		{"正文前有内容保留", "前言<think>思考</think>正文", "前言正文"},
		{"未闭合原样", "<think>思考忘了闭合的正文", "<think>思考忘了闭合的正文"},
		{"无标签原样", "普通回复", "普通回复"},
		{"只有闭合标签原样", "正文</think>尾部", "正文</think>尾部"},
	}
	for _, c := range cases {
		if got := StripThinking(c.in); got != c.want {
			t.Errorf("%s: StripThinking(%q) = %q, 期望 %q", c.name, c.in, got, c.want)
		}
	}
}

// CoTGuideFor：空自定义用内置默认；非空自定义替换文本并保留【输出格式要求】头。
func TestCoTGuideFor(t *testing.T) {
	if got := CoTGuideFor(""); got != DefaultCoTGuide {
		t.Fatal("空自定义应返回内置默认指导")
	}
	if got := CoTGuideFor("  "); got != DefaultCoTGuide {
		t.Fatal("纯空白自定义应返回内置默认指导")
	}
	custom := CoTGuideFor("自定义指导")
	if custom == DefaultCoTGuide {
		t.Fatal("自定义指导应覆盖默认文本")
	}
	if got := custom[:len("【输出格式要求】")]; got != "【输出格式要求】" {
		t.Fatalf("自定义指导应保留【输出格式要求】头: %q", custom)
	}
}
