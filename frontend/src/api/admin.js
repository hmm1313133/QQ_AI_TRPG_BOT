import { ElMessage } from 'element-plus'

const TOKEN_KEY = 'trpg_admin_token'

export function getAdminToken() {
  // 注意：空串是合法选择（本机无 ADMIN_TOKEN 模式），只有从未设置过（null）才弹框
  let token = sessionStorage.getItem(TOKEN_KEY)
  if (token === null) {
    token = window.prompt('请输入管理令牌（本机访问可留空）:') || ''
    sessionStorage.setItem(TOKEN_KEY, token)
  }
  return token
}

/**
 * 管理后台 API 封装：自动携带 Bearer token，401/403 时清除令牌并提示。
 * @param {string} path
 * @param {RequestInit} opts
 */
export async function adminApi(path, opts = {}) {
  const token = getAdminToken()
  const headers = Object.assign({ Authorization: 'Bearer ' + token }, opts.headers)
  const resp = await fetch(path, { ...opts, headers })
  if (resp.status === 401 || resp.status === 403) {
    sessionStorage.removeItem(TOKEN_KEY)
    ElMessage.error('鉴权失败，请刷新页面重新输入令牌')
    throw new Error('unauthorized')
  }
  return resp.json()
}

/**
 * 带错误处理的请求封装：非 2xx 时抛出 Error（message 为后端错误文本，status 为 HTTP 状态码）。
 * 后端错误响应对 http.Error（text/plain），故不能用 adminApi 的 resp.json()。
 * @param {string} path
 * @param {RequestInit} opts
 */
export async function adminReq(path, opts = {}) {
  const token = getAdminToken()
  const headers = Object.assign({ Authorization: 'Bearer ' + token }, opts.headers)
  const resp = await fetch(path, { ...opts, headers })
  if (resp.status === 401 || resp.status === 403) {
    sessionStorage.removeItem(TOKEN_KEY)
    ElMessage.error('鉴权失败，请刷新页面重新输入令牌')
    throw new Error('unauthorized')
  }
  const text = await resp.text()
  if (!resp.ok) {
    const err = new Error(text.trim() || `请求失败（${resp.status}）`)
    err.status = resp.status
    throw err
  }
  return text ? JSON.parse(text) : null
}

// ============================================================
// 世界设定库（lore）与分区编辑（《世界设定库与按需加载设计.md》§4.4）
// ============================================================

const enc = encodeURIComponent

export const worldApi = {
  list: () => adminReq('/api/admin/worlds'),
  detail: (id) => adminReq(`/api/admin/worlds/${enc(id)}`),
  create: (body) => adminReq('/api/admin/worlds', { method: 'POST', body: JSON.stringify(body) }),
  remove: (id) => adminReq(`/api/admin/worlds/${enc(id)}`, { method: 'DELETE' }),
  advance: (id) => adminReq(`/api/admin/worlds/${enc(id)}/advance`, { method: 'POST' }),
  section: (id, part) => adminReq(`/api/admin/worlds/${enc(id)}/section?part=${enc(part)}`),
  saveSection: (id, part, data) => adminReq(`/api/admin/worlds/${enc(id)}/section`, {
    method: 'PATCH',
    body: JSON.stringify({ part, data }),
  }),
}

// ============================================================
// 素材联动与游玩存档（《世界编辑器与素材联动设计.md》§四/§9.4）
// ============================================================

export const assetApi = {
  // 素材目录：{ library, cards, worlds, scripts }
  catalog: () => adminReq('/api/admin/assets'),
  // 素材库列表（可选 kind / 关键词 q / 标签 tag 过滤）
  list: ({ kind = '', q = '', tag = '' } = {}) => {
    const params = new URLSearchParams()
    if (kind) params.set('kind', kind)
    if (q) params.set('q', q)
    if (tag) params.set('tag', tag)
    const qs = params.toString()
    return adminReq('/api/admin/assets/library' + (qs ? '?' + qs : ''))
  },
  create: (body) => adminReq('/api/admin/assets/library', { method: 'POST', body: JSON.stringify(body) }),
  get: (aid) => adminReq(`/api/admin/assets/library/${enc(aid)}`),
  update: (aid, body) => adminReq(`/api/admin/assets/library/${enc(aid)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  }),
  remove: (aid) => adminReq(`/api/admin/assets/library/${enc(aid)}`, { method: 'DELETE' }),
  // 把世界实体收藏进素材库
  collect: (worldId, body) => adminReq(`/api/admin/worlds/${enc(worldId)}/assets/collect`, {
    method: 'POST',
    body: JSON.stringify(body),
  }),
  // 素材导入世界：{ library, cards, copy, script_characters, on_conflict }
  import: (worldId, body) => adminReq(`/api/admin/worlds/${enc(worldId)}/assets/import`, {
    method: 'POST',
    body: JSON.stringify(body),
  }),
  // 素材解析（文件上传：SillyTavern 角色卡 PNG/JSON、txt/md，multipart 字段名 file）。
  // 错误响应为 text/plain（http.Error），adminReq 已读 text 并抛出，e.message 即后端错误文本。
  parse: (file) => {
    const fd = new FormData()
    fd.append('file', file)
    return adminReq('/api/admin/assets/parse', { method: 'POST', body: fd })
  },
  // 素材解析（粘贴文本，JSON body）
  parseText: (text) => adminReq('/api/admin/assets/parse', {
    method: 'POST',
    body: JSON.stringify({ text }),
  }),
  // 批量入库：{ assets: [{kind, name, tags, summary, source, payload}] } -> { created, errors }
  batchCreate: (assets) => adminReq('/api/admin/assets/library/batch', {
    method: 'POST',
    body: JSON.stringify({ assets }),
  }),
}

export const scriptApi = {
  // 剧本素材一键入库：派生背景/角色/场景/组织/主线素材 -> { created, skipped, errors }
  collectAssets: (id) => adminReq(`/api/admin/scripts/${enc(id)}/assets/collect`, { method: 'POST' }),
}

export const saveApi = {
  list: (worldId) => adminReq(`/api/admin/worlds/${enc(worldId)}/saves`),
  create: (worldId, body) => adminReq(`/api/admin/worlds/${enc(worldId)}/saves`, {
    method: 'POST',
    body: JSON.stringify(body),
  }),
  restore: (worldId, sid) => adminReq(`/api/admin/worlds/${enc(worldId)}/saves/${enc(String(sid))}/restore`, {
    method: 'POST',
  }),
  remove: (worldId, sid) => adminReq(`/api/admin/worlds/${enc(worldId)}/saves/${enc(String(sid))}`, {
    method: 'DELETE',
  }),
}

/**
 * 玩家侧存档请求（Web 聊天页）：不带管理令牌，用 ?auth= 聊天令牌 + ?token= 会话令牌。
 * 404 表示当前会话没有进行中的世界，抛出 err.status 供调用方识别。
 */
export async function playerSaveReq(path, { token, auth = '', ...opts } = {}) {
  const params = new URLSearchParams({ auth, token })
  const sep = path.includes('?') ? '&' : '?'
  const resp = await fetch(path + sep + params.toString(), opts)
  const text = await resp.text()
  if (!resp.ok) {
    const err = new Error(text.trim() || `请求失败（${resp.status}）`)
    err.status = resp.status
    throw err
  }
  return text ? JSON.parse(text) : null
}

export const loreApi = {
  list: (id) => adminReq(`/api/admin/worlds/${enc(id)}/lore`),
  create: (id, entry) => adminReq(`/api/admin/worlds/${enc(id)}/lore`, {
    method: 'POST',
    body: JSON.stringify(entry),
  }),
  update: (id, eid, entry) => adminReq(`/api/admin/worlds/${enc(id)}/lore/${enc(eid)}`, {
    method: 'PUT',
    body: JSON.stringify(entry),
  }),
  remove: (id, eid) => adminReq(`/api/admin/worlds/${enc(id)}/lore/${enc(eid)}`, { method: 'DELETE' }),
  test: (id, text) => adminReq(`/api/admin/worlds/${enc(id)}/lore/test`, {
    method: 'POST',
    body: JSON.stringify({ text }),
  }),
  injections: (id) => adminReq(`/api/admin/worlds/${enc(id)}/lore/injections`),
  importText: (id, text) => adminReq(`/api/admin/worlds/${enc(id)}/lore/import`, {
    method: 'POST',
    body: JSON.stringify({ text }),
  }),
}
