// Narrator messages 结构与工具循环测试。
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestBuildNarratorMessages(t *testing.T) {
	input := &NarratorInput{
		ContextBlock: "【玩家人设】\n羽儿: 身体健壮",
		History: []world.DialogueTurn{
			{Player: "我推开柴房门", Narrator: "辉跪在泥地上。"},
			{Player: "", Narrator: "（空玩家消息应跳过）"}, // 开场叙述轮
		},
		PlayerName:    "羽儿",
		PlayerMessage: "你抬起头来",
		Tail:          "【回复风格要求】\n平实",
	}
	msgs := buildNarratorMessages("系统提示词", input)

	if msgs[0].Role != model.RoleSystem || msgs[0].Content != "系统提示词" {
		t.Fatalf("首条应为 system 主提示词: %+v", msgs[0])
	}
	if msgs[1].Role != model.RoleUser || !strings.Contains(msgs[1].Content, "玩家人设") {
		t.Fatalf("次条应为设定资料 user 消息: %+v", msgs[1])
	}
	// 历史消息对：空玩家消息跳过，其叙事仍保留
	if msgs[2].Role != model.RoleUser || msgs[2].Content != "我推开柴房门" {
		t.Fatalf("历史玩家消息: %+v", msgs[2])
	}
	if msgs[3].Role != model.RoleAssistant || msgs[3].Content != "辉跪在泥地上。" {
		t.Fatalf("历史叙事消息: %+v", msgs[3])
	}
	if msgs[4].Role != model.RoleAssistant {
		t.Fatalf("空玩家消息应跳过但叙事保留: %+v", msgs[4])
	}
	last := msgs[len(msgs)-1]
	if last.Role != model.RoleUser {
		t.Fatalf("末条应为本轮玩家 user 消息: %+v", last)
	}
	if !strings.Contains(last.Content, "你抬起头来") || !strings.Contains(last.Content, "【回复风格要求】") {
		t.Fatalf("尾部指令区应拼接在玩家消息之后: %q", last.Content)
	}
	if strings.Contains(last.Content, "玩家人设") {
		t.Fatal("设定资料不应混入最终玩家消息")
	}
}

// fakeModel 按队列返回预设响应，记录每次请求的消息列表。
type fakeModel struct {
	responses []*model.Response
	calls     [][]model.Message
}

func (f *fakeModel) Info() model.Info { return model.Info{Name: "fake"} }

func (f *fakeModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	f.calls = append(f.calls, append([]model.Message(nil), req.Messages...))
	ch := make(chan *model.Response, 1)
	if len(f.responses) > 0 {
		ch <- f.responses[0]
		f.responses = f.responses[1:]
	}
	close(ch)
	return ch, nil
}

func completionMsg(content string) *model.Response {
	return &model.Response{
		Object:  "chat.completion",
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
	}
}

// 工具循环：第一轮模型要求 roll_dice，执行后第二轮返回叙事；
// 第二次请求应携带 assistant 的 tool_calls 与 tool 结果消息。
func TestNarratorGenerateToolLoop(t *testing.T) {
	fake := &fakeModel{responses: []*model.Response{
		{
			Object: "chat.completion",
			Choices: []model.Choice{{Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: model.FunctionDefinitionParam{
						Name:      "roll_dice",
						Arguments: []byte(`{"expression":"1d100","reason":"侦查"}`),
					},
				}},
			}}},
		},
		completionMsg("你掷出了 42 点。"),
	}}
	n := &Narrator{model: fake, tools: []tool.Tool{NewRollDiceTool(nil)}}

	reply, _, err := n.generate(context.Background(), []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "我侦查一下"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "你掷出了 42 点。" {
		t.Fatalf("最终回复: %q", reply)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("应调用模型 2 次: %d", len(fake.calls))
	}
	// 第二轮请求：原始消息 + assistant(tool_calls) + tool(结果)
	second := fake.calls[1]
	if len(second) != 4 {
		t.Fatalf("第二轮消息数: %d", len(second))
	}
	if len(second[2].ToolCalls) != 1 {
		t.Fatalf("assistant 消息应带 tool_calls: %+v", second[2])
	}
	toolMsg := second[3]
	if toolMsg.Role != model.RoleTool || toolMsg.ToolID != "call_1" || toolMsg.ToolName != "roll_dice" {
		t.Fatalf("tool 结果消息: %+v", toolMsg)
	}
	if !strings.Contains(toolMsg.Content, `"total"`) {
		t.Fatalf("tool 结果应为 roll_dice JSON: %s", toolMsg.Content)
	}
	// 骰点应记账到会话（sessionMgr 为 nil 时不记账，仅验证不 panic）
}

// 无工具调用：一次请求直接返回。
func TestNarratorGenerateNoTool(t *testing.T) {
	fake := &fakeModel{responses: []*model.Response{completionMsg("风平浪静。")}}
	n := &Narrator{model: fake, tools: []tool.Tool{NewRollDiceTool(nil)}}

	reply, _, err := n.generate(context.Background(), []model.Message{
		{Role: model.RoleUser, Content: "我休息"},
	})
	if err != nil || reply != "风平浪静。" {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("应只调用 1 次: %d", len(fake.calls))
	}
}

// API 级错误应向上返回。
func TestNarratorGenerateAPIError(t *testing.T) {
	fake := &fakeModel{responses: []*model.Response{{
		Error: &model.ResponseError{Message: "rate limited"},
	}}}
	n := &Narrator{model: fake}
	if _, _, err := n.generate(context.Background(), []model.Message{{Role: model.RoleUser, Content: "x"}}); err == nil {
		t.Fatal("API 错误应返回 error")
	}
}
