import { ElMessage } from 'element-plus'

const TOKEN_KEY = 'trpg_admin_token'

export function getAdminToken() {
  let token = sessionStorage.getItem(TOKEN_KEY) || ''
  if (!token) {
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
