package qqbot

import "testing"

func TestWSClient_StatsZeroValue(t *testing.T) {
	c := NewWSClient(nil, nil, NewEventDispatcher(), 0)
	st := c.Stats()
	if st.Connected {
		t.Fatal("未连接时 Connected 应为 false")
	}
	if st.RxCount != 0 || st.TxCount != 0 || st.ReconnectCount != 0 {
		t.Fatalf("计数器应为零: %+v", st)
	}
	if !st.LastConnectedAt.IsZero() {
		t.Fatalf("从未连接时 LastConnectedAt 应为零值: %v", st.LastConnectedAt)
	}
}

func TestBot_StatsPassthrough(t *testing.T) {
	b := NewBot(&Config{AppID: "appid", ClientSecret: "secret"})
	st := b.Stats()
	if st.Connected || st.RxCount != 0 || st.TxCount != 0 {
		t.Fatalf("初始统计应为零值: %+v", st)
	}
}
