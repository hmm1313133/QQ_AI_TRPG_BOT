package qqbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// WSClient 是 QQ 机器人 WebSocket 事件订阅客户端。
// 实现: 连接 → Hello → Identify → 心跳循环 → 事件分发 → 断线 Resume
type WSClient struct {
	tokenMgr    *tokenManager
	api         *OpenAPI
	dispatcher  *EventDispatcher
	intents     Intent

	// 连接状态
	mu         sync.Mutex
	conn       *websocket.Conn
	sessionID  string
	lastSeq    int64          // 最后处理的消息序列号
	connected  atomic.Bool    // 是否已连接
	ctx        context.Context
	cancel     context.CancelFunc

	// 心跳
	heartbeatInterval time.Duration
	heartbeatTicker   *time.Ticker
}

// NewWSClient 创建 WebSocket 客户端。
func NewWSClient(tokenMgr *tokenManager, api *OpenAPI, dispatcher *EventDispatcher, intents Intent) *WSClient {
	return &WSClient{
		tokenMgr:   tokenMgr,
		api:        api,
		dispatcher: dispatcher,
		intents:    intents,
	}
}

// Run 启动 WebSocket 客户端，阻塞直到 ctx 取消。
// 内部自动处理断线重连。
func (w *WSClient) Run(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	defer w.cancel()

	for {
		if w.ctx.Err() != nil {
			return w.ctx.Err()
		}

		err := w.connectAndServe(w.ctx)
		if err != nil {
			log.Printf("[WS] 连接异常: %v", err)
		}

		// 判断是否需要 Resume
		needResume := w.sessionID != "" && w.lastSeq > 0

		// 退避重连
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()
		case <-time.After(3 * time.Second):
		}

		if needResume {
			log.Printf("[WS] 尝试 Resume 会话, sessionID=%s, seq=%d", w.sessionID, w.lastSeq)
		} else {
			log.Printf("[WS] 尝试重新连接...")
		}
	}
}

// Stop 停止 WebSocket 客户端。
func (w *WSClient) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		w.conn.Close(websocket.StatusNormalClosure, "client closing")
	}
}

// connectAndServe 建立连接并处理消息，直到连接断开。
func (w *WSClient) connectAndServe(ctx context.Context) error {
	// 1. 获取 WSS 地址
	gateway, err := w.api.GetGatewayBot(ctx)
	if err != nil {
		return fmt.Errorf("获取 gateway 失败: %w", err)
	}

	// 2. 建立 WebSocket 连接
	conn, _, err := websocket.Dial(ctx, gateway.URL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 连接失败: %w", err)
	}
	// 设置读取限制 (10MB)
	conn.SetReadLimit(10 * 1024 * 1024)

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()
	defer func() {
		conn.Close(websocket.StatusInternalError, "closing")
		w.connected.Store(false)
	}()

	// 3. 等待 Hello (op=10)
	hello, err := w.receivePayload(ctx)
	if err != nil {
		return fmt.Errorf("等待 Hello 失败: %w", err)
	}
	if hello.Op != OpHello {
		return fmt.Errorf("期望 Hello (op=10), 收到 op=%d", hello.Op)
	}

	helloData, err := parseData[HelloData](hello.D)
	if err != nil {
		return fmt.Errorf("解析 Hello 数据失败: %w", err)
	}
	w.heartbeatInterval = time.Duration(helloData.HeartbeatInterval) * time.Millisecond
	log.Printf("[WS] 收到 Hello, 心跳间隔=%v", w.heartbeatInterval)

	// 4. 发送 Identify 或 Resume
	if w.sessionID != "" {
		if err := w.sendResume(ctx); err != nil {
			return fmt.Errorf("发送 Resume 失败: %w", err)
		}
	} else {
		if err := w.sendIdentify(ctx); err != nil {
			return fmt.Errorf("发送 Identify 失败: %w", err)
		}
	}

	// 5. 启动心跳
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go w.heartbeatLoop(heartbeatCtx)

	w.connected.Store(true)
	log.Printf("[WS] 连接已建立，开始接收事件")

	// 6. 消息接收循环
	for {
		payload, err := w.receivePayload(ctx)
		if err != nil {
			return fmt.Errorf("接收消息失败: %w", err)
		}

		switch payload.Op {
		case OpDispatch:
			w.handleDispatch(payload)
		case OpHeartbeat:
			// 服务端要求立即发送心跳
			w.sendHeartbeat(ctx)
		case OpReconnect:
			log.Printf("[WS] 服务端要求重新连接")
			return fmt.Errorf("服务端要求 Reconnect")
		case OpInvalidSession:
			log.Printf("[WS] 无效会话，需重新 Identify")
			w.sessionID = ""
			w.lastSeq = 0
			return fmt.Errorf("无效会话")
		case OpHeartbeatACK:
			// 心跳确认，无需处理
		default:
			log.Printf("[WS] 收到未知 opcode: %d", payload.Op)
		}
	}
}

// sendIdentify 发送 OpCode 2 Identify 鉴权消息。
func (w *WSClient) sendIdentify(ctx context.Context) error {
	token, err := w.tokenMgr.authHeader(ctx)
	if err != nil {
		return err
	}

	payload := Payload{
		Op: OpIdentify,
		D: IdentifyData{
			Token:   token,
			Intents: w.intents,
			Shard:   []int{0, 1}, // 默认单分片
			Properties: map[string]interface{}{
				"$os":      "linux",
				"$browser": "QQ_AI_TRPG_BOT",
				"$device":  "QQ_AI_TRPG_BOT",
			},
		},
	}
	return w.sendPayload(ctx, payload)
}

// sendResume 发送 OpCode 6 Resume 恢复会话消息。
func (w *WSClient) sendResume(ctx context.Context) error {
	token, err := w.tokenMgr.authHeader(ctx)
	if err != nil {
		return err
	}

	payload := Payload{
		Op: OpResume,
		D: ResumeData{
			Token:     token,
			SessionID: w.sessionID,
			Seq:       w.lastSeq,
		},
	}
	return w.sendPayload(ctx, payload)
}

// heartbeatLoop 按周期发送心跳。
func (w *WSClient) heartbeatLoop(ctx context.Context) {
	w.heartbeatTicker = time.NewTicker(w.heartbeatInterval)
	defer w.heartbeatTicker.Stop()

	// 首次心跳
	w.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.heartbeatTicker.C:
			w.sendHeartbeat(ctx)
		}
	}
}

// sendHeartbeat 发送 OpCode 1 心跳消息。
func (w *WSClient) sendHeartbeat(ctx context.Context) {
	var d interface{}
	if w.lastSeq > 0 {
		d = w.lastSeq
	} else {
		d = nil
	}

	payload := Payload{
		Op: OpHeartbeat,
		D:  d,
	}
	if err := w.sendPayload(ctx, payload); err != nil {
		log.Printf("[WS] 发送心跳失败: %v", err)
	}
}

// handleDispatch 处理服务端推送的 Dispatch 事件 (op=0)。
func (w *WSClient) handleDispatch(payload *Payload) {
	// 更新最新序列号
	if payload.S > 0 {
		atomic.StoreInt64(&w.lastSeq, payload.S)
	}

	switch payload.T {
	case EventReady:
		ready, err := parseData[ReadyEvent](payload.D)
		if err != nil {
			log.Printf("[WS] 解析 READY 事件失败: %v", err)
			return
		}
		w.sessionID = ready.SessionID
		log.Printf("[WS] 鉴权成功, sessionID=%s, bot=%s", ready.SessionID, ready.User.Username)

	case EventResumed:
		log.Printf("[WS] 会话恢复成功")

	default:
		// 解析并分发事件
		data := w.parseEventData(payload.T, payload.D)
		ctx := &EventContext{
			EventType: payload.T,
			Seq:       payload.S,
			EventID:   payload.ID,
			RawData:   data,
			API:       w.api,
		}
		w.dispatcher.Dispatch(ctx)
	}
}

// parseEventData 根据事件类型解析 payload.d 字段。
func (w *WSClient) parseEventData(eventType string, raw interface{}) interface{} {
	// 先将 raw 序列化为 JSON 再反序列化为目标类型
	data, err := json.Marshal(raw)
	if err != nil {
		log.Printf("[WS] 序列化事件数据失败: %v", err)
		return raw
	}

	switch eventType {
	case EventC2CMessageCreate:
		var msg C2CMessageEvent
		if err := json.Unmarshal(data, &msg); err == nil {
			return &msg
		}
	case EventGroupAtMessageCreate, EventGroupMessageCreate:
		var msg GroupMessageEvent
		if err := json.Unmarshal(data, &msg); err == nil {
			return &msg
		}
	case EventAtMessageCreate, EventDirectMessageCreate:
		var msg ChannelMessageEvent
		if err := json.Unmarshal(data, &msg); err == nil {
			return &msg
		}
	case EventInteractionCreate:
		var evt InteractionEvent
		if err := json.Unmarshal(data, &evt); err == nil {
			return &evt
		}
	case EventFriendAdd, EventFriendDel:
		var evt FriendEvent
		if err := json.Unmarshal(data, &evt); err == nil {
			return &evt
		}
	case EventGroupAddRobot, EventGroupDelRobot:
		var evt GroupRobotEvent
		if err := json.Unmarshal(data, &evt); err == nil {
			return &evt
		}
	}
	return raw
}

// receivePayload 从 WebSocket 读取一条消息并解析为 Payload。
func (w *WSClient) receivePayload(ctx context.Context) (*Payload, error) {
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn == nil {
		return nil, fmt.Errorf("连接未建立")
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}

	var payload Payload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("解析 payload 失败: %w, raw: %s", err, string(data))
	}
	return &payload, nil
}

// sendPayload 发送一条 Payload 到 WebSocket。
func (w *WSClient) sendPayload(ctx context.Context, payload Payload) error {
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 payload 失败: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// parseData 将 payload.D (interface{}) 解析为目标类型。
func parseData[T any](d interface{}) (T, error) {
	var result T
	data, err := json.Marshal(d)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}
