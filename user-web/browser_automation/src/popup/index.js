// popup/index.js — 可视化操作面板：登录 / 任务列表 / 执行 / 监控 / API 地址配置
import {
  loadConfig, saveConfig, getConfiguredBaseUrl, testConnection,
  isLoggedIn, login, logout, listTasks, runTask, getSession, getSessionSteps, getHostStatus,
} from '../core/api-client.js';
import { DEFAULT_USER_SERVER, normalizeServerUrl } from '../core/constants.js';

const $ = (id) => document.getElementById(id);
const banner = $('banner');

function showBanner(type, msg) {
  banner.className = `banner ${type}`;
  banner.textContent = msg;
  if (type !== 'error') setTimeout(() => { banner.className = 'banner'; }, 4000);
}

// ---- Host 状态 ----

async function refreshHostStatus() {
  const dot = $('hostDot');
  const line = $('hostLine');
  try {
    if (!(await isLoggedIn())) {
      dot.className = 'dot off';
      line.textContent = 'Host 状态：请先登录';
      return;
    }
    const st = await getHostStatus();
    const online = (st?.count || 0) > 0;
    dot.className = `dot ${online ? 'on' : 'off'}`;
    line.textContent = online
      ? `Host 已连接（${st.count} 台在线）`
      : 'Host 离线：请确认本机 NM Host 已安装（~/.hivemtk/nm_host.conf 有 token）';
  } catch (e) {
    dot.className = 'dot off';
    line.textContent = `Host 状态获取失败：${e.message}`;
  }
}

// ---- 任务列表 + 执行 ----

const STATUS_TAG = { running: 'running', done: 'done', completed: 'done', failed: 'failed', ready: 'ready' };

async function loadTasks() {
  const box = $('taskList');
  try {
    const data = await listTasks('page=1&limit=20');
    const tasks = data?.list || [];
    if (!tasks.length) {
      box.innerHTML = '<div class="hint">暂无任务。请在 user-web 前端「浏览器自动化 → 任务列表」创建。</div>';
      return;
    }
    box.innerHTML = '';
    tasks.forEach((t) => {
      const div = document.createElement('div');
      div.className = 'task';
      const tagCls = STATUS_TAG[t.status] || '';
      div.innerHTML = `
        <div class="t-name">#${t.id} ${escapeHtml(t.name)}</div>
        <div class="t-meta">
          <span class="tag ${tagCls}">${t.status}</span>
          <span>${t.task_type}${t.brain_mode ? ' · Brain' : ''}</span>
          <button data-run="${t.id}" ${t.status === 'running' ? 'disabled' : ''}>执行</button>
        </div>`;
      box.appendChild(div);
    });
    box.querySelectorAll('button[data-run]').forEach((btn) => {
      btn.addEventListener('click', () => onRunTask(btn.dataset.run));
    });
  } catch (e) {
    box.innerHTML = `<div class="hint">任务加载失败：${escapeHtml(e.message)}</div>`;
  }
}

let pollTimer = null;

async function onRunTask(taskId) {
  try {
    const res = await runTask(taskId);
    const sessionId = res?.session_id;
    if (sessionId) await chrome.storage.local.set({ lastSessionId: sessionId });
    showBanner('success', `任务 #${taskId} 已开始执行（Session ${sessionId}）`);
    monitorSession(sessionId);
    setTimeout(loadTasks, 800);
  } catch (e) {
    showBanner('error', `执行失败：${e.message}`);
  }
}

// ---- 执行监控（2s 轮询到终态）----

function monitorSession(sessionId) {
  if (pollTimer) clearInterval(pollTimer);
  $('monitor').style.display = 'block';
  $('monSessionId').textContent = sessionId;
  const tick = async () => {
    try {
      const [sess, stepsRes] = await Promise.all([getSession(sessionId), getSessionSteps(sessionId)]);
      const steps = stepsRes?.list || stepsRes || [];
      $('monSteps').innerHTML =
        `<b>${sess.status}</b>` +
        (sess.error_msg ? ` · ${escapeHtml(sess.error_msg)}` : '') +
        '<br/>' +
        steps.map((s) => `${s.step_index + 1}. ${s.action} <span class="tag ${STATUS_TAG[s.status] || ''}">${s.status}</span>`).join('<br/>');
      if (['completed', 'failed', 'stopped'].includes(sess.status)) {
        clearInterval(pollTimer);
        pollTimer = null;
        loadTasks();
      }
    } catch (e) {
      $('monSteps').textContent = `监控失败：${e.message}`;
    }
  };
  tick();
  pollTimer = setInterval(tick, 2000);
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// ---- 登录/退出 ----

async function refreshAuthUI() {
  const loggedIn = await isLoggedIn();
  $('loginSection').style.display = loggedIn ? 'none' : 'block';
  $('mainSection').style.display = loggedIn ? 'block' : 'none';
  if (loggedIn) {
    loadTasks();
    monitorFromStorage();
  }
  refreshHostStatus();
}

async function monitorFromStorage() {
  // popup 重开时恢复对最近 session 的监控
  try {
    const res = await chrome.storage.local.get('lastSessionId');
    const sid = res?.lastSessionId;
    if (!sid) return;
    const sess = await getSession(sid);
    if (sess && ['created', 'active'].includes(sess.status)) monitorSession(sid);
  } catch { /* 无可恢复 */ }
}

$('loginBtn').addEventListener('click', async () => {
  const btn = $('loginBtn');
  btn.disabled = true;
  try {
    const user = await login($('serverUrl').value, $('username').value.trim(), $('password').value);
    showBanner('success', `欢迎，${user?.username || '用户'}`);
    $('password').value = '';
    await refreshAuthUI();
  } catch (e) {
    showBanner('error', e.message);
  } finally {
    btn.disabled = false;
  }
});

$('logoutBtn').addEventListener('click', async () => {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  await logout();
  $('mainSection').style.display = 'none';
  $('loginSection').style.display = 'block';
  refreshHostStatus();
});

$('refreshBtn').addEventListener('click', loadTasks);

// ---- 设置区：API 地址（保存 + 默认值 + 测试连接）----

$('saveBtn').addEventListener('click', async () => {
  const raw = $('serverUrl').value.trim();
  if (raw && !/^https?:\/\//i.test(normalizeServerUrl(raw))) {
    showBanner('error', '地址需以 http(s):// 开头');
    return;
  }
  try {
    await saveConfig({ serverUrl: normalizeServerUrl(raw) });
    showBanner('success', `✓ 已保存。当前生效地址：${await getConfiguredBaseUrl()}`);
  } catch (e) {
    showBanner('error', `保存失败：${e.message}`);
  }
});

$('testBtn').addEventListener('click', async () => {
  const btn = $('testBtn');
  btn.disabled = true;
  try {
    const r = await testConnection($('serverUrl').value);
    if (r.ok) {
      showBanner('success', r.degraded
        ? `服务端可达但降级（${r.base}${r.path}），PG/Redis 可能有故障`
        : `✓ 连接成功：${r.base}${r.path}`);
    } else {
      showBanner('error', `无法连接 ${r.base}，请确认 user-server 已启动`);
    }
  } finally {
    btn.disabled = false;
  }
});

// ---- 初始化 ----

(async function init() {
  $('serverUrl').placeholder = DEFAULT_USER_SERVER.baseUrl;
  const cfg = await loadConfig();
  $('serverUrl').value = cfg.serverUrl || '';
  await refreshAuthUI();
  // 打开 popup 期间若有监控在跑，关窗自动清 timer（popup 生命周期即页面生命周期）
  window.addEventListener('pagehide', () => { if (pollTimer) clearInterval(pollTimer); });
})();
