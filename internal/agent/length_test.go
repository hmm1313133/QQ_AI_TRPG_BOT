package agent

import (
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// 档位映射：三档均有注入文本，空/未知 pref 返回空串。
func TestLengthHint(t *testing.T) {
	for _, pref := range []string{ReplyLengthShort, ReplyLengthStandard, ReplyLengthDetailed} {
		hint := LengthHint(pref)
		if hint == "" || !strings.HasPrefix(hint, "【回复长度要求】") {
			t.Fatalf("档位 %s 应有注入文本: %q", pref, hint)
		}
	}
	if !strings.Contains(LengthHint(ReplyLengthShort), "150") {
		t.Fatal("short 档应要求 150 字以内")
	}
	if !strings.Contains(LengthHint(ReplyLengthStandard), "300") {
		t.Fatal("standard 档应要求 300 字左右")
	}
	if !strings.Contains(LengthHint(ReplyLengthDetailed), "600") {
		t.Fatal("detailed 档应要求 600 字左右")
	}
	if LengthHint("") != "" || LengthHint("verbose") != "" {
		t.Fatal("空/未知 pref 应返回空串")
	}
}

// 中文别名归一化。
func TestNormalizeReplyLength(t *testing.T) {
	cases := map[string]string{
		"short": ReplyLengthShort, "standard": ReplyLengthStandard, "detailed": ReplyLengthDetailed,
		"简短": ReplyLengthShort, "标准": ReplyLengthStandard, "详细": ReplyLengthDetailed,
	}
	for in, want := range cases {
		if got := NormalizeReplyLength(in); got != want {
			t.Fatalf("NormalizeReplyLength(%q) = %q, 期望 %q", in, got, want)
		}
	}
	if NormalizeReplyLength("啰嗦") != "" || NormalizeReplyLength("") != "" {
		t.Fatal("无法识别的输入应返回空串")
	}
}

// 会话级偏好存取与注入文本生成。
func TestSessionReplyLength(t *testing.T) {
	s := core.NewSessionManager().GetSession("s1")
	if SessionReplyLength(s) != "" || LengthHintFromSession(s) != "" {
		t.Fatal("未设置时应返回空串")
	}
	SetSessionReplyLength(s, ReplyLengthShort)
	if SessionReplyLength(s) != ReplyLengthShort {
		t.Fatal("设置后应能读回档位")
	}
	if LengthHintFromSession(s) != LengthHint(ReplyLengthShort) {
		t.Fatal("注入文本应与档位映射一致")
	}
	// nil 会话安全
	if SessionReplyLength(nil) != "" || LengthHintFromSession(nil) != "" {
		t.Fatal("nil 会话应返回空串")
	}
}
