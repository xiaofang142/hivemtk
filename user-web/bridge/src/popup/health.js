
import { CHANNEL_DISPLAY, UI_DEFAULTS } from '../core/constants.js';

// HTML 转义（popup XSS 收口：channel 键/state/错误码/失败原因/accountId
// 均可源自第三方页面 DOM 或后端回传字符串，禁止原样进 innerHTML 模板）
// 导出供 index.js 复用；accounts.js/error-messages.js 保有其历史局部定义。
export function escapeHtml(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// 状态机颜色映射（与 popup banner 风格统一）
const STATE_COLORS = {
  CLOSED: '#16a34a',     
  OPEN: '#dc2626',       
  HALF_OPEN: '#d97706',  
  unknown: '#6b7280',    
};

const STATE_LABELS = {
  CLOSED: '正常',
  OPEN: '熔断中',
  HALF_OPEN: '探测中',
  unknown: '无数据',
};

// 渠道展示名（统一单源）
function channelDisplayName(channel) {
  if (!channel) return '';
  return CHANNEL_DISPLAY[channel] || channel;
}

// 时间戳格式化为 HH:MM:SS（仅时分秒，避免冗长）
export function fmtTime(ts) {
  if (!ts) return '-';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '-';
  return d.toLocaleTimeString('zh-CN', { hour12: false });
}

// 毫秒转可读时长
export function fmtAgoMs(ms) {
  if (ms == null) return '-';
  if (ms < 1000) return `${ms}ms 前`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s 前`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m 前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h 前`;
  const d = Math.floor(h / 24);
  return `${d}d 前`;
}

// 渲染单个渠道的健康卡片
function renderChannelCard(channel, h) {
  const state = h.state || 'unknown';
  const color = STATE_COLORS[state] || STATE_COLORS.unknown;
  const label = STATE_LABELS[state] || state;
  const now = Date.now();
  const lastSuccessAgo = h.lastSuccessAt ? fmtAgoMs(now - h.lastSuccessAt) : '从未';
  const lastFailureAgo = h.lastFailureAt ? fmtAgoMs(now - h.lastFailureAt) : '从未';
  const healthy = h.healthy === true;
  const healthyBadge = healthy
    ? '<span style="color:#16a34a;font-weight:600;">● 健康</span>'
    : '<span style="color:#dc2626;font-weight:600;">● 异常</span>';

  // 错码分布（聚合）
  const errDist = h.errorCodeDistribution || {};
  const errSummary = Object.entries(errDist)
    .filter(([, n]) => n > 0)
    .map(([k, n]) => `${escapeHtml(k)}:${escapeHtml(n)}`)
    .join(' / ') || '无';

  // 延迟分位
  const lat = h.latencyMs || {};
  const latSummary = lat.count
    ? `P50 ${escapeHtml(lat.p50)}ms / P95 ${escapeHtml(lat.p95)}ms / max ${escapeHtml(lat.max)}ms`
    : '无采样';

  // 累计
  const totals = h.totals || { calls: 0, ok: 0, fail: 0, okRate: 0 };
  const okRateStr = totals.calls
    ? `${(Number(totals.okRate) * 100).toFixed(1)}%`
    : '-';

  // 最近失败原因（最多 5 条）——错误消息常嵌第三方页面文本，必须转义
  const reasons = (h.recentReasons || []).slice(0, 3)
    .map(r => `  - ${escapeHtml(String(r).slice(0, 80))}`)
    .join('\n') || '  (无)';

  // 幂等键
  const idem = h.idempotency || { keysTracked: 0, dedupeHits: 0 };
  const idemStr = `已跟踪 ${escapeHtml(idem.keysTracked)} 键 / 去重命中 ${escapeHtml(idem.dedupeHits)}`;

  return [
    `<div class="health-card" data-channel="${escapeHtml(channel)}" style="border:1px solid #e5e7eb;border-radius:6px;padding:8px 10px;margin-bottom:6px;background:#fafafa;">`,
    `  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px;">`,
    `    <strong style="font-size:13px;">${escapeHtml(channelDisplayName(channel))}</strong>`,
    `    ${healthyBadge}`,
    `  </div>`,
    `  <div style="font-size:12px;line-height:1.5;">`,
    `    <div>熔断器: <span style="color:${color};font-weight:600;">${escapeHtml(label)}</span>（失败 ${escapeHtml(h.failureCount || 0)} 次）</div>`,
    `    <div>最近成功: ${lastSuccessAgo}（${fmtTime(h.lastSuccessAt)}）</div>`,
    `    <div>最近失败: ${lastFailureAgo}（${fmtTime(h.lastFailureAt)}）</div>`,
    `    <div>调用: ${escapeHtml(totals.calls)}（成功 ${escapeHtml(totals.ok)} / 失败 ${escapeHtml(totals.fail)}，成功率 ${okRateStr}）</div>`,
    `    <div>延迟: ${latSummary}</div>`,
    `    <div>错码: ${errSummary}</div>`,
    `    <div>幂等: ${idemStr}</div>`,
    `  </div>`,
    `  <details style="margin-top:4px;font-size:11px;">`,
    `    <summary style="cursor:pointer;color:#6b7280;">最近失败原因（最多 3 条）</summary>`,
    `    <pre style="margin:4px 0 0;padding:6px;background:#fff;border:1px solid #e5e7eb;border-radius:4px;white-space:pre-wrap;word-break:break-all;">${reasons}</pre>`,
    `  </details>`,
    `</div>`,
  ].join('\n');
}

// 渲染聚合健康面板
// data: { [channel]: { state, failureCount, lastSuccessAt, lastFailureAt, ... } }
// 返回 HTML 字符串；调用方写入容器
export function renderHealthPanel(data) {
  if (!data || typeof data !== 'object') {
    return '<div class="health-empty" style="color:#6b7280;font-size:12px;padding:8px;">暂无健康度数据（content script 未启动或未上报）</div>';
  }
  const channels = Object.keys(data);
  if (channels.length === 0) {
    return '<div class="health-empty" style="color:#6b7280;font-size:12px;padding:8px;">暂无健康度数据（content script 未启动或未上报）</div>';
  }
  // 按渠道名排序（抖音/小红书/TikTok/闲鱼 顺序）
  const order = ['douyin', 'xiaohongshu', 'tiktok', 'xianyu', 'kuaishou'];
  const sorted = channels.sort((a, b) => {
    const ai = order.indexOf(a); const bi = order.indexOf(b);
    if (ai === -1 && bi === -1) return a.localeCompare(b);
    if (ai === -1) return 1; if (bi === -1) return -1;
    return ai - bi;
  });
  return sorted.map(ch => renderChannelCard(ch, data[ch])).join('\n');
}

// 判定是否有任意渠道需要告警（用于 banner 联动）
// 返回 null 表示无告警；否则返回首个告警的简述
export function detectHealthAlert(data) {
  if (!data || typeof data !== 'object') return null;
  const now = Date.now();
  for (const [channel, h] of Object.entries(data)) {
    if (!h) continue;
    if (h.state === 'OPEN') {
      return {
        level: 'error',
        channel,
        title: `${channelDisplayName(channel)} 桥接已熔断`,
        body: '5 次连续失败，已停止请求 30s。可点击下方「自检」排查。',
      };
    }
    if (h.healthy === false) {
      const lastSuccessAt = h.lastSuccessAt || 0;
      const deadSec = lastSuccessAt ? Math.floor((now - lastSuccessAt) / 1000) : -1;
      return {
        level: 'error',
        channel,
        title: `${channelDisplayName(channel)} 桥接无响应`,
        body: `已 ${deadSec}s 无成功请求，超过死开关阈值（${h.deadManSeconds || 300}s）。`,
      };
    }
    if (h.state === 'HALF_OPEN') {
      return {
        level: 'warn',
        channel,
        title: `${channelDisplayName(channel)} 桥接正在恢复`,
        body: '熔断器探测中，下一次成功将恢复正常。',
      };
    }
  }
  return null;
}

// 拉取 background 聚合的 health
// 返回 Promise<{ [channel]: snapshot }>
export function fetchHealth() {
  return new Promise((resolve) => {
    try {
      chrome.runtime.sendMessage({ type: 'getStatus' }, (res) => {
        try {
          if (chrome.runtime.lastError) {
            resolve({});
            return;
          }
        } catch (_) {  }
        if (!res || typeof res !== 'object') {
          resolve({});
          return;
        }
        resolve(res.health || {});
      });
    } catch (_) {
      resolve({});
    }
  });
}

// 启动 popup 健康面板轮询
// opts: { containerId, intervalMs? }
// 返回 stop 函数
export function startHealthPanelPolling(opts) {
  const { containerId, intervalMs = UI_DEFAULTS.popupHealthPanelPollMs } = opts || {};
  if (!containerId) return () => {};
  const container = () => document.getElementById(containerId);
  let stopped = false;
  const tick = async () => {
    if (stopped) return;
    const el = container();
    if (!el) return;
    try {
      const data = await fetchHealth();
      el.innerHTML = renderHealthPanel(data);
    } catch (e) {
      el.innerHTML = `<div style="color:#dc2626;font-size:12px;padding:8px;">健康面板拉取失败：${escapeHtml(e && e.message ? e.message : String(e))}</div>`;
    }
  };
  tick();
  const timer = setInterval(tick, intervalMs);
  return () => {
    stopped = true;
    if (timer) clearInterval(timer);
  };
}

// 停止 popup 健康面板轮询（占位，便于对称；与 startHealthPanelPolling 返回的 stop 一致）
export function stopHealthPanelPolling(stop) {
  if (typeof stop === 'function') stop();
}

if (typeof window !== 'undefined') {
  window.__healthPanel = {
    renderHealthPanel,
    detectHealthAlert,
    fetchHealth,
    fmtAgoMs,
    fmtTime,
    channelDisplayName,
    STATE_COLORS,
    STATE_LABELS,
  };
}

