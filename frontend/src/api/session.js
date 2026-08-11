/**
 * 聊天会话令牌工具：与 ChatView 的 ensureToken / chatAuth 逻辑一致。
 * token 存 localStorage('trpg_token')（缺失时向 /api/session 申领），
 * auth 为部署方开启 ChatToken 时通过 URL ?auth= 带入的聊天访问令牌。
 */

let cachedToken = localStorage.getItem('trpg_token') || ''

export async function ensureChatToken() {
  if (cachedToken) return cachedToken
  const resp = await fetch('/api/session')
  const data = await resp.json()
  cachedToken = data.token
  localStorage.setItem('trpg_token', cachedToken)
  return cachedToken
}

export function getChatAuth() {
  return new URLSearchParams(location.search).get('auth') || ''
}
