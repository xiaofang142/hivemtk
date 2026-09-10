// api-client.js — 扩展侧 user-server API 客户端
// 职责：配置存取（chrome.storage.local，默认 http://localhost:8204）+ JWT 登录态管理 + 业务调用。
// 响应信封：{code, message, data}，code===0 为成功；拦截不存在，这里统一拆包。

import { DEFAULT_USER_SERVER, STORAGE_KEY, AUTH_KEY, normalizeServerUrl } from './constants.js';

// ---- 配置存取（保存的服务端地址，支持默认值）----

export function loadConfig() {
  return new Promise((resolve) => {
    try {
      chrome.storage.local.get(STORAGE_KEY, (res) => {
        const cfg = res && res[STORAGE_KEY];
        resolve(cfg || { serverUrl: '' }); // 空 = 用默认值
      });
    } catch {
      resolve({ serverUrl: '' });
    }
  });
}

export function saveConfig(cfg) {
  return new Promise((resolve, reject) => {
    try {
      chrome.storage.local.set({ [STORAGE_KEY]: cfg }, () => {
        const err = chrome.runtime.lastError;
        if (err) return reject(new Error(err.message));
        resolve(true);
      });
    } catch (e) {
      reject(e);
    }
  });
}

export function getConfiguredBaseUrl() {
  return loadConfig().then((cfg) => normalizeServerUrl(cfg.serverUrl) || DEFAULT_USER_SERVER.baseUrl);
}

// ---- 登录态 ----

function loadAuth() {
  return new Promise((resolve) => {
    try {
      chrome.storage.local.get(AUTH_KEY, (res) => resolve((res && res[AUTH_KEY]) || {}));
    } catch {
      resolve({});
    }
  });
}

function saveAuth(auth) {
  return new Promise((resolve) => {
    try {
      chrome.storage.local.set({ [AUTH_KEY]: auth }, () => resolve(true));
    } catch {
      resolve(false);
    }
  });
}

export async function isLoggedIn() {
  const auth = await loadAuth();
  if (!auth.token) return false;
  // JWT exp 秒级时间戳；提前 60s 视为过期
  if (auth.exp && auth.exp * 1000 < Date.now() + 60000) return false;
  return true; // 剩余不足时由 ensureFreshToken 静默续期
}

export async function login(serverUrlOverride, username, password) {
  const base = normalizeServerUrl(serverUrlOverride) || (await getConfiguredBaseUrl());
  const res = await fetch(`${base}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const body = await res.json().catch(() => ({}));
  if (body.code !== 0 || !body.data?.token) {
    throw new Error(body.message || `登录失败（HTTP ${res.status}）`);
  }
  // JWT payload 解 exp（不验签——信任本机 server）
  let exp = 0;
  try {
    const payload = JSON.parse(atob(body.data.token.split('.')[1]));
    exp = payload.exp || 0;
  } catch { /* exp 缺失时每次都重新登录 */ }
  // 密码不落盘。续期用 server 已有的 refresh-token 接口（旧 token 换新，24h 滚动），
  // 效果 = 登录一次长期有效（见 ensureFreshToken）。
  await saveAuth({ token: body.data.token, exp, username, serverUrl: base });
  return body.data.user || { username };
}

export async function logout() {
  await saveAuth({});
}

// ---- 业务调用（带 JWT；401 时自动重登一次）----

// refreshTokenOnce 用旧 token 换新并落盘；旧 token 服务端即入黑名单，必须原子替换
async function refreshTokenOnce(auth) {
  try {
    const base = auth.serverUrl || (await getConfiguredBaseUrl());
    const res = await fetch(`${base}/api/auth/refresh-token`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${auth.token}` },
    });
    if (res.status === 401) return false; // 已过期太久/进黑名单，只能重登
    const body = await res.json().catch(() => ({}));
    if (body.code !== 0 || !body.data?.token) return false;
    let exp = 0;
    try {
      exp = JSON.parse(atob(body.data.token.split('.')[1])).exp || 0;
    } catch { /* 无 exp 时下次仍走 401 兜底 */ }
    await saveAuth({ ...auth, token: body.data.token, exp });
    return true;
  } catch {
    return false;
  }
}

// ensureFreshToken：exp 剩余 <1h 时主动续期。服务端 24h 滚动窗口 + 扩展每次使用前检查
// => 只要在 24h 内用过一次扩展就永久有效；超过 24h 未用才需要重新登录（符合个人电脑
//    常驻场景，又避免无限期 token 的安全风险）。
async function ensureFreshToken(auth) {
  if (auth.exp && auth.exp * 1000 < Date.now() + 3600 * 1000) {
    await refreshTokenOnce(auth);
  }
}

async function apiCall(path, { method = 'GET', body, retry = true } = {}) {
  const auth = await loadAuth();
  const base = auth.serverUrl || (await getConfiguredBaseUrl());
  if (!auth.token) throw new Error('未登录：请在扩展弹窗中先登录');
  await ensureFreshToken(auth);

  const res = await fetch(`${base}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${auth.token}` },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  // 401 兜底：服务端 nbf 保护下 refresh 偶发竞态，重登一次；仍失败则要求手动登录
  if (res.status === 401 && retry) {
    if (auth.username && auth.password) {
      // 兼容旧 storage 里可能残留的记住密码：静默重登一次后清掉密码
      try {
        await login(auth.serverUrl || base, auth.username, auth.password);
        const cur = await loadAuth();
        await saveAuth({ ...cur, password: undefined });
        return await apiCall(path, { method, body, retry: false });
      } catch {
        await saveAuth({ username: auth.username });
        throw new Error('自动重登失败，请在弹窗中重新登录');
      }
    }
    if (auth.token && (await refreshTokenOnce(auth))) {
      return await apiCall(path, { method, body, retry: false });
    }
    await saveAuth({ username: auth.username });
    throw new Error('登录已过期，请在弹窗中重新登录');
  }

  const data = await res.json().catch(() => ({}));
  if (data.code !== 0) {
    const err = new Error(data.message || `API ${path} 失败（HTTP ${res.status}）`);
    err.code = data.code;
    throw err;
  }
  return data.data;
}

// ---- 健康检查（测试连接按钮）----

export async function testConnection(serverUrlOverride) {
  const base = normalizeServerUrl(serverUrlOverride) || (await getConfiguredBaseUrl());
  for (const p of DEFAULT_USER_SERVER.healthPaths) {
    try {
      const ctl = new AbortController();
      const t = setTimeout(() => ctl.abort(), 5000);
      const res = await fetch(`${base}${p}`, { signal: ctl.signal });
      clearTimeout(t);
      if (res.ok) return { ok: true, base, path: p };
      if (res.status >= 500) return { ok: true, degraded: true, base, path: p };
    } catch {
      // 试下一个路径
    }
  }
  return { ok: false, base };
}

// ---- 业务 API ----

export const listTasks = (params = '') => apiCall(`/api/browser-automation/tasks?${params}`);
export const getTask = (id) => apiCall(`/api/browser-automation/tasks/${id}`);
export const createTask = (data) => apiCall('/api/browser-automation/tasks', { method: 'POST', body: data });
export const runTask = (id) => apiCall(`/api/browser-automation/tasks/${id}/run`, { method: 'POST', body: {} });
export const getSession = (id) => apiCall(`/api/browser-automation/sessions/${id}`);
export const getSessionSteps = (id) => apiCall(`/api/browser-automation/sessions/${id}/steps`);
export const getHostStatus = () => apiCall('/api/browser-automation/host/status');
