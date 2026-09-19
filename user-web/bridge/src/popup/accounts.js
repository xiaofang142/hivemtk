import { createLogger } from '../core/logger.js';
import { CHANNEL_DISPLAY } from '../core/constants.js';
import { configStore, normalizeAccounts } from '../core/config-store.js';

const log = createLogger('accounts', 'popup');
const STORAGE_KEY = 'bridgeConfig';

function channelDisplayName(channel) {
  if (!channel) return '';
  return CHANNEL_DISPLAY[channel] || channel;
}

function escapeHtml(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// 读取已配置账号启停状态（优先 storage 最新值，回退 configStore 内存）
export function fetchAccountStates() {
  return new Promise((resolve) => {
    try {
      if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
        chrome.storage.local.get(STORAGE_KEY, (res) => {
          try {
            if (chrome.runtime.lastError) { resolve({}); return; }
          } catch (_) {  }
          const cfg = (res && res[STORAGE_KEY]) || {};
          resolve(normalizeAccounts(cfg.accounts));
        });
        return;
      }
    } catch (_) {  }
    resolve(normalizeAccounts(configStore.getConfig().accounts));
  });
}

// 暂停/启用某渠道某账号：优先走 background 消息（持久化 + 广播内容脚本），
// 失败时兜底直写 configStore（仍会持久化到 storage）。
export function setAccountEnabledViaBackground(channel, accountId, enabled) {
  return new Promise((resolve) => {
    let called = false;
    const done = (r) => { if (!called) { called = true; resolve(r); } };
    try {
      if (typeof chrome !== 'undefined' && chrome.runtime && chrome.runtime.sendMessage) {
        chrome.runtime.sendMessage(
          { type: 'bridge:setAccountEnabled', channel, accountId, enabled: !!enabled },
          (resp) => {
            try {
              if (chrome.runtime.lastError) {
                done({ ok: false, error: chrome.runtime.lastError.message || 'background 无响应' });
                return;
              }
            } catch (_) {  }
            if (resp && resp.ok) { done({ ok: true }); return; }
            done({ ok: false, error: (resp && resp.error) || 'background 处理失败' });
          }
        );
        return;
      }
    } catch (_) {  }
    configStore.setAccountEnabled(channel, accountId, !!enabled).then(
      () => done({ ok: true, fallback: true }),
      (e) => done({ ok: false, error: String((e && e.message) || e) })
    );
  });
}

// 渲染账号启停列表 HTML（纯函数，便于测试）
// states: { channel: { accountId: { enabled, pausedAt } } }
export function renderAccountRows(states) {
  const order = ['douyin', 'xiaohongshu', 'tiktok', 'xianyu', 'kuaishou'];
  const channels = Object.keys(states || {}).sort((a, b) => {
    const ai = order.indexOf(a); const bi = order.indexOf(b);
    if (ai === -1 && bi === -1) return a.localeCompare(b);
    if (ai === -1) return 1; if (bi === -1) return -1;
    return ai - bi;
  });
  const rows = [];
  for (const channel of channels) {
    const ch = states[channel] || {};
    const accountIds = Object.keys(ch);
    for (const accountId of accountIds) {
      const st = ch[accountId] || {};
      const enabled = st.enabled !== false;
      const pausedAt = st.pausedAt || null;
      const statusText = enabled ? '启用' : '暂停';
      const statusClass = enabled ? 'enabled' : 'paused';
      const pausedHint = pausedAt
        ? ` · 暂停于 ${new Date(pausedAt).toLocaleTimeString('zh-CN', { hour12: false })}`
        : '';
      rows.push(
        `<div class="account-state-row" data-channel="${escapeHtml(channel)}" data-account="${escapeHtml(accountId)}">` +
        `  <div class="dot ${statusClass}"></div>` +
        `  <div class="name">${escapeHtml(channelDisplayName(channel))}</div>` +
        `  <div class="meta">${escapeHtml(accountId)}${pausedHint}</div>` +
        `  <button class="toggle ${enabled ? 'danger' : 'primary'}" data-channel="${escapeHtml(channel)}" data-account="${escapeHtml(accountId)}" data-enable="${enabled ? 'false' : 'true'}">${statusText === '启用' ? '暂停' : '启用'}</button>` +
        `</div>`
      );
    }
  }
  if (!rows.length) {
    return '<div class="hint" style="padding:8px;">暂无已配置账号（配置导入 / 多账号上线后在此显示启停开关）</div>';
  }
  return rows.join('\n');
}

// 渲染到容器并绑定切换事件（事件委托，避免重复绑定）
// opts: { onToggle(channel, accountId, enable, btn) }
export async function renderAccountsList(container, opts = {}) {
  if (!container) return;
  const states = await fetchAccountStates();
  container.innerHTML = renderAccountRows(states);
  if (!container._acctClickBound && typeof container.addEventListener === 'function') {
    container._acctClickBound = true;
    container.addEventListener('click', async (ev) => {
      const btn = ev.target && ev.target.closest ? ev.target.closest('button.toggle') : null;
      if (!btn) return;
      const channel = btn.getAttribute('data-channel');
      const accountId = btn.getAttribute('data-account');
      const enable = btn.getAttribute('data-enable') === 'true';
      if (!channel || !accountId) return;
      if (typeof opts.onToggle === 'function') {
        await opts.onToggle(channel, accountId, enable, btn);
      }
    });
  }
}

if (typeof window !== 'undefined') {
  window.__accounts = {
    fetchAccountStates,
    setAccountEnabledViaBackground,
    renderAccountRows,
    renderAccountsList,
    normalizeAccounts,
  };
}

