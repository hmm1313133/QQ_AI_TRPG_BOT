// Package web - 玩家侧剧情摘要 API（Web 聊天页）。
//
// 剧情摘要已直接迁移到长期记忆（world_memory 的 episode 块）：
// 对话滑出 RecentTurns 窗口后由 SummaryService 攒批总结写入记忆表，
// 注入上下文时按时间顺序列出，替代远期对话窗口。
// 玩家可查看、修改、删除记忆条目，也可手动补录。
//
//   - GET    /api/chat/summary                摘要条目列表 + 待总结轮数
//   - POST   /api/chat/summary/entries        手动补录一条（{text}）
//   - PUT    /api/chat/summary/entries/{id}   修改条目文本（{text}）
//   - DELETE /api/chat/summary/entries/{id}   删除条目
package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/memory"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// registerChatSummaryRoutes 注册玩家侧剧情摘要路由（buildMux 中调用）。
func (s *Server) registerChatSummaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/chat/summary", s.handleChatSummaryGet)
	mux.HandleFunc("POST /api/chat/summary/entries", s.handleChatSummaryAdd)
	mux.HandleFunc("PUT /api/chat/summary/entries/{id}", s.handleChatSummaryEdit)
	mux.HandleFunc("DELETE /api/chat/summary/entries/{id}", s.handleChatSummaryDelete)
}

// handleChatSummaryGet 返回当前会话世界的摘要条目列表与待总结轮数。
func (s *Server) handleChatSummaryGet(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	entries := make([]memory.Chunk, 0, 8)
	if store := s.adminDeps.Memory; store != nil {
		// 时间线 = 篇章摘要（level=2）+ 未归档阶段摘要，已归档的不再单列。
		list, err := store.Timeline(sessionID, 0, 0)
		if err != nil {
			writeChatError(w, "读取剧情摘要失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, list...)
		// 旧世界首次打开摘要面板时顺手完成迁移（与回合内迁移共用去重键）。
		if len(entries) == 0 {
			if chunks := agent.LegacySummaryChunks(ws); len(chunks) > 0 {
				if _, err := store.AppendBatch(chunks); err != nil {
					log.Printf("[Web] 旧摘要迁移失败: world=%s: %v", sessionID, err)
				}
				if list, err := store.Episodes(sessionID, 0); err == nil {
					entries = append(entries, list...)
				}
				if len(entries) > 0 {
					ws.Summaries = nil
					ws.CampaignSummary = ""
					if err := s.adminDeps.WorldEngine.Save(ws); err != nil {
						log.Printf("[Web] 旧摘要迁移后清理失败: world=%s: %v", sessionID, err)
					}
				}
			}
		}
	}
	// 无记忆存储（旧部署/测试）时回退只读视图。
	if len(entries) == 0 {
		for _, e := range ws.SummaryEntries() {
			entries = append(entries, memory.Chunk{
				ID: int64(e.ID), RoundFrom: e.RoundFrom, RoundTo: e.RoundTo,
				Text: e.Text, CreatedAt: e.CreatedAt,
			})
		}
	}
	writeJSON(w, map[string]any{
		"entries":       entries,
		"pending_turns": len(ws.PendingSummaryTurns),
	})
}

// chatSummaryTextReq 摘要条目文本请求体。
type chatSummaryTextReq struct {
	Text string `json:"text"`
}

// chatSummaryWorld 校验会话与世界并加世界锁（调用方负责解锁），返回会话 ID 与世界状态。
func (s *Server) chatSummaryWorld(w http.ResponseWriter, r *http.Request) (string, *world.WorldState, bool) {
	sessionID, ok := s.chatSessionContext(w, r)
	if !ok {
		return "", nil, false
	}
	if !s.adminReady || s.adminDeps.WorldEngine == nil {
		writeChatError(w, "世界引擎不可用", http.StatusServiceUnavailable)
		return "", nil, false
	}
	engine := s.adminDeps.WorldEngine
	engine.Lock(sessionID)
	ws := engine.LoadOrNil(sessionID)
	if ws == nil {
		engine.Unlock(sessionID)
		writeChatError(w, "当前会话还没有进行中的世界", http.StatusNotFound)
		return "", nil, false
	}
	return sessionID, ws, true
}

// handleChatSummaryAdd 手动补录一条摘要（追加到列表末尾；轮次范围为 0 表示手动条目）。
func (s *Server) handleChatSummaryAdd(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	var req chatSummaryTextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeChatError(w, "text 不能为空", http.StatusBadRequest)
		return
	}

	var id int64
	if store := s.adminDeps.Memory; store != nil {
		newID, err := store.Append(memory.Chunk{
			WorldID: sessionID, Kind: memory.KindEpisode, Level: memory.LevelChunk,
			Text: text, Importance: 7, CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		})
		if err != nil {
			writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		id = newID
	} else {
		id = int64(ws.AppendSummary(text, 0, 0, ""))
	}
	if err := saveWorldWithAuditNote(s.adminDeps.WorldEngine, ws, "player",
		"section:summary", fmt.Sprintf("玩家补录剧情摘要 #%d", id)); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// chatSummaryEntryID 解析路径中的条目 id。
func chatSummaryEntryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var id int64
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil || id <= 0 {
		writeChatError(w, "条目 ID 无效", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// handleChatSummaryEdit 修改一条摘要的文本。
func (s *Server) handleChatSummaryEdit(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	id, ok := chatSummaryEntryID(w, r)
	if !ok {
		return
	}
	var req chatSummaryTextReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeChatError(w, "text 不能为空", http.StatusBadRequest)
		return
	}

	if store := s.adminDeps.Memory; store != nil {
		found, err := store.UpdateText(sessionID, id, text)
		if err != nil {
			writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			writeChatError(w, "摘要条目不存在", http.StatusNotFound)
			return
		}
	} else if !ws.EditSummary(int(id), text) {
		writeChatError(w, "摘要条目不存在", http.StatusNotFound)
		return
	}
	if err := saveWorldWithAuditNote(s.adminDeps.WorldEngine, ws, "player",
		"section:summary", fmt.Sprintf("玩家编辑剧情摘要 #%d", id)); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// handleChatSummaryDelete 删除一条摘要。
func (s *Server) handleChatSummaryDelete(w http.ResponseWriter, r *http.Request) {
	sessionID, ws, ok := s.chatSummaryWorld(w, r)
	if !ok {
		return
	}
	defer s.adminDeps.WorldEngine.Unlock(sessionID)

	id, ok := chatSummaryEntryID(w, r)
	if !ok {
		return
	}
	if store := s.adminDeps.Memory; store != nil {
		found, err := store.Delete(sessionID, id)
		if err != nil {
			writeChatError(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			writeChatError(w, "摘要条目不存在", http.StatusNotFound)
			return
		}
	} else if !ws.DeleteSummary(int(id)) {
		writeChatError(w, "摘要条目不存在", http.StatusNotFound)
		return
	}
	if err := saveWorldWithAuditNote(s.adminDeps.WorldEngine, ws, "player",
		"section:summary", fmt.Sprintf("玩家删除剧情摘要 #%d", id)); err != nil {
		writeChatError(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}
