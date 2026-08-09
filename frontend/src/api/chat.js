import { playerSaveReq } from './admin'

/**
 * 玩家侧聊天会话 API（Web 聊天页）：与 playerSaveReq 同一鉴权方式，
 * 即 ?auth= 聊天访问令牌 + ?token= 会话令牌（见 admin.js playerSaveReq）。
 */

// 会话状态：{ mode, reply_length, world_id, world_name, persona }
export function getChatState(token, auth = '') {
  return playerSaveReq('/api/chat/state', { token, auth })
}

// 更新会话状态（可选字段：mode / reply_length），返回完整 state
export function updateChatState(token, auth = '', patch = {}) {
  return playerSaveReq('/api/chat/state', {
    token,
    auth,
    method: 'PUT',
    body: JSON.stringify(patch),
  })
}

// 查询人设：world 为空串表示全局默认人设，否则为指定世界的专属人设
export function getPersona(token, auth = '', world = '') {
  return playerSaveReq('/api/chat/persona?world=' + encodeURIComponent(world), { token, auth })
}

// 设置人设：description 为空串 = 清除；world 为空串 = 全局人设
export function setPersona(token, auth = '', { world = '', name = '', description = '' } = {}) {
  return playerSaveReq('/api/chat/persona', {
    token,
    auth,
    method: 'PUT',
    body: JSON.stringify({ world, name, description }),
  })
}

// 进入世界（实例化复制到本会话）
export function enterWorld(token, auth = '', worldId) {
  return playerSaveReq('/api/chat/world/enter', {
    token,
    auth,
    method: 'POST',
    body: JSON.stringify({ world_id: worldId }),
  })
}

// 离开当前世界（清除会话内世界进度）
export function leaveWorld(token, auth = '') {
  return playerSaveReq('/api/chat/world/leave', { token, auth, method: 'POST' })
}

// 编辑 AI 回复文本（修改的是当前激活版本）
export function editChatMessage(token, auth = '', id, text) {
  return playerSaveReq(`/api/chat/messages/${id}`, {
    token,
    auth,
    method: 'PUT',
    body: JSON.stringify({ text }),
  })
}

// 切换 AI 回复的激活版本，返回 { ok, text, version_no, version_count }
export function switchChatMessageVersion(token, auth = '', id, versionNo) {
  return playerSaveReq(`/api/chat/messages/${id}/version`, {
    token,
    auth,
    method: 'PUT',
    body: JSON.stringify({ version_no: versionNo }),
  })
}
