// 轻量 API 封装：自动带 token，统一错误
const state = {
  token: localStorage.getItem('token') || '',
  user: JSON.parse(localStorage.getItem('user') || 'null')
}

export function setAuth(token, user) {
  state.token = token
  state.user = user
  localStorage.setItem('token', token)
  localStorage.setItem('user', JSON.stringify(user))
}

export function clearAuth() {
  state.token = ''
  state.user = null
  localStorage.removeItem('token')
  localStorage.removeItem('user')
}

export function getUser() { return state.user }

export async function api(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) }
  if (state.token) headers['Authorization'] = 'Bearer ' + state.token
  const res = await fetch('/api' + path, { ...options, headers })
  let data = null
  try { data = await res.json() } catch (e) { /* ignore */ }
  if (!res.ok) {
    const err = new Error((data && (data.error || data.message)) || ('请求失败 ' + res.status))
    err.status = res.status
    err.data = data
    throw err
  }
  return data
}

export const get = (p) => api(p)
export const post = (p, body) => api(p, { method: 'POST', body: JSON.stringify(body || {}) })
