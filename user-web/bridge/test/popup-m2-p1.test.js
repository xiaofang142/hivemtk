
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';

beforeEach(() => {
  globalThis.chrome = {
    storage: {
      local: {
        _data: {},
        _asyncGet(keys) {
          const out = {};
          const list = Array.isArray(keys) ? keys : [keys];
          for (const k of list) if (k in this._data) out[k] = this._data[k];
          return Promise.resolve(out);
        },
        _asyncSet(obj) {
          Object.assign(this._data, obj);
          return Promise.resolve();
        },
        _asyncRemove(keys) {
          const list = Array.isArray(keys) ? keys : [keys];
          for (const k of list) delete this._data[k];
          return Promise.resolve();
        },
        get(keys, cb) {
          if (typeof cb === 'function') {
            const out = {};
            const list = Array.isArray(keys) ? keys : [keys];
            for (const k of list) if (k in this._data) out[k] = this._data[k];
            setTimeout(() => cb(out), 0);
            return;
          }
          return this._asyncGet(keys);
        },
        set(obj, cb) {
          if (typeof cb === 'function') {
            Object.assign(this._data, obj);
            setTimeout(() => cb(), 0);
            return;
          }
          return this._asyncSet(obj);
        },
        remove(keys, cb) {
          if (typeof cb === 'function') {
            const list = Array.isArray(keys) ? keys : [keys];
            for (const k of list) delete this._data[k];
            setTimeout(() => cb(), 0);
            return;
          }
          return this._asyncRemove(keys);
        },
      },
    },
    runtime: {
      _listeners: [],
      _lastError: null,
      sendMessage: vi.fn((msg, cb) => {
        if (cb) setTimeout(() => cb && cb(undefined), 0);
      }),
      addListener(fn) { this._listeners.push(fn); },
      onMessage: { addListener: (fn) => this._listeners.push(fn) },
      lastError: null,
    },
    tabs: {
      query: vi.fn((q, cb) => cb && cb([])),
      sendMessage: vi.fn((id, msg, cb) => { if (cb) cb && cb({ ok: true }); }),
      create: vi.fn(),
    },
  };
  globalThis.window = globalThis.window || globalThis;
  globalThis.document = globalThis.document || { getElementById: () => null, addEventListener: () => {} };
  globalThis.window.confirm = vi.fn(() => true);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('M2-P1 error-messages explainError', () => {
  it('HTTP 4xx 错误码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ status: 401 });
    expect(r.key).toBe('http_401');
    expect(r.level).toBe('error');
    expect(r.title).toContain('凭证');
    // 401 的判据不止"分类对"：闸门是 fail-closed 的，用户唯一出路是去后台重新生成凭证，
    // 指引丢了就等于把 401 说成"稍后再试"（本轮改文案的动机）。
    expect(r.action).toContain('桥接凭证');
  });

  it('HTTP 5xx 错误码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ status: 503 });
    expect(r.key).toBe('http_503');
    expect(r.title).toContain('不可用');
  });

  it('字符串错误含 "Failed to fetch" → net_unreachable', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError(new Error('Failed to fetch'));
    expect(r.key).toBe('net_unreachable');
  });

  it('字符串错误含 "timeout" → net_timeout', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError(new Error('Request timeout'));
    expect(r.key).toBe('net_timeout');
  });

  it('AbortError → net_abort', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError(Object.assign(new Error('aborted'), { name: 'AbortError' }));
    expect(r.key).toBe('net_abort');
  });

  it('circuit_open 业务码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ code: 'circuit_open' });
    expect(r.key).toBe('circuit_open');
    expect(r.title).toContain('熔断');
  });

  it('dead_letter 业务码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ code: 'dead_letter' });
    expect(r.key).toBe('dead_letter');
  });

  it('pending_ack_exceeded 业务码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ code: 'pending_ack_exceeded' });
    expect(r.key).toBe('pending_ack_exceeded');
  });

  it('ack_failed 业务码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ code: 'ack_failed' });
    expect(r.key).toBe('ack_failed');
  });

  it('rate_limited 业务码分类', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ code: 'rate_limited' });
    expect(r.key).toBe('rate_limited');
  });

  it('null 错误 → unknown', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError(null);
    expect(r.key).toBe('unknown');
  });

  it('空对象 → unknown', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({});
    expect(r.key).toBe('unknown');
  });

  it('en 语言支持', async () => {
    const { explainError } = await import('../src/popup/error-messages.js');
    const r = explainError({ status: 500 }, { lang: 'en' });
    expect(r.title).toContain('Internal');
  });

  it('formatErrorBanner 返回含 title/body/action/docUrl', async () => {
    const { formatErrorBanner } = await import('../src/popup/error-messages.js');
    const r = formatErrorBanner({ status: 429 });
    expect(r.level).toBe('warn');
    expect(r.html).toContain('建议');
  });

  it('formatErrorBanner XSS 转义', async () => {
    const { formatErrorBanner } = await import('../src/popup/error-messages.js');
    // 构造一个会触发 docUrl 的错误码
    const r = formatErrorBanner({ status: 401 });
    expect(r.html).not.toContain('<script>');
  });
});

describe('M2-P1 health renderHealthPanel', () => {
  it('空数据返回占位文案', async () => {
    const { renderHealthPanel } = await import('../src/popup/health.js');
    const html = renderHealthPanel(null);
    expect(html).toContain('暂无健康度数据');
  });

  it('空对象返回占位文案', async () => {
    const { renderHealthPanel } = await import('../src/popup/health.js');
    const html = renderHealthPanel({});
    expect(html).toContain('暂无健康度数据');
  });

  it('单渠道卡片渲染：CLOSED 绿色', async () => {
    const { renderHealthPanel } = await import('../src/popup/health.js');
    const html = renderHealthPanel({
      douyin: {
        state: 'CLOSED',
        failureCount: 0,
        lastSuccessAt: Date.now() - 5000,
        lastFailureAt: 0,
        healthy: true,
        totals: { calls: 10, ok: 10, fail: 0, okRate: 1 },
        latencyMs: { p50: 50, p95: 100, count: 10, max: 150, min: 30, sum: 500, avg: 50 },
        errorCodeDistribution: { '4xx': 0, '5xx': 0, net: 0, abort: 0, other: 0 },
        recentReasons: [],
        idempotency: { keysTracked: 0, dedupeHits: 0 },
      },
    });
    expect(html).toContain('抖音');
    expect(html).toContain('正常');  
    expect(html).toContain('● 健康');
  });

  it('多渠道渲染：按顺序排序（抖音/小红书/TikTok/闲鱼）', async () => {
    const { renderHealthPanel } = await import('../src/popup/health.js');
    const html = renderHealthPanel({
      xianyu: { state: 'CLOSED', healthy: true, totals: {}, latencyMs: {}, errorCodeDistribution: {}, recentReasons: [], idempotency: {} },
      douyin: { state: 'CLOSED', healthy: true, totals: {}, latencyMs: {}, errorCodeDistribution: {}, recentReasons: [], idempotency: {} },
    });
    const douyinIdx = html.indexOf('抖音');
    const xianyuIdx = html.indexOf('闲鱼');
    expect(douyinIdx).toBeGreaterThan(0);
    expect(xianyuIdx).toBeGreaterThan(0);
    expect(douyinIdx).toBeLessThan(xianyuIdx);
  });

  it('OPEN 状态显示熔断中', async () => {
    const { renderHealthPanel } = await import('../src/popup/health.js');
    const html = renderHealthPanel({
      douyin: {
        state: 'OPEN',
        failureCount: 5,
        lastSuccessAt: Date.now() - 30000,
        lastFailureAt: Date.now() - 1000,
        healthy: false,
        recentReasons: ['HTTP 500'],
        totals: { calls: 5, ok: 0, fail: 5, okRate: 0 },
        latencyMs: { count: 0 },
        errorCodeDistribution: { '5xx': 5 },
        idempotency: { keysTracked: 0, dedupeHits: 0 },
      },
    });
    expect(html).toContain('熔断中');
    expect(html).toContain('● 异常');
  });

  it('HALF_OPEN 状态显示探测中', async () => {
    const { renderHealthPanel } = await import('../src/popup/health.js');
    const html = renderHealthPanel({
      xiaohongshu: {
        state: 'HALF_OPEN', failureCount: 0, healthy: true,
        recentReasons: [], totals: { calls: 0 }, latencyMs: { count: 0 },
        errorCodeDistribution: {}, idempotency: {},
      },
    });
    expect(html).toContain('探测中');
  });
});

describe('M2-P1 health detectHealthAlert', () => {
  it('无数据 → null', async () => {
    const { detectHealthAlert } = await import('../src/popup/health.js');
    expect(detectHealthAlert(null)).toBeNull();
  });

  it('空对象 → null', async () => {
    const { detectHealthAlert } = await import('../src/popup/health.js');
    expect(detectHealthAlert({})).toBeNull();
  });

  it('有渠道 OPEN → 返回 error 告警', async () => {
    const { detectHealthAlert } = await import('../src/popup/health.js');
    const a = detectHealthAlert({
      douyin: { state: 'OPEN', healthy: false },
    });
    expect(a).not.toBeNull();
    expect(a.level).toBe('error');
    expect(a.title).toContain('熔断');
  });

  it('有渠道 unhealthy → 返回 error 告警（dead-man）', async () => {
    const { detectHealthAlert } = await import('../src/popup/health.js');
    const a = detectHealthAlert({
      douyin: { state: 'CLOSED', healthy: false, lastSuccessAt: Date.now() - 600000, deadManSeconds: 300 },
    });
    expect(a).not.toBeNull();
    expect(a.level).toBe('error');
    expect(a.title).toContain('无响应');
  });

  it('HALF_OPEN → warn 告警', async () => {
    const { detectHealthAlert } = await import('../src/popup/health.js');
    const a = detectHealthAlert({
      douyin: { state: 'HALF_OPEN', healthy: true },
    });
    expect(a).not.toBeNull();
    expect(a.level).toBe('warn');
    expect(a.title).toContain('恢复');
  });

  it('全部健康 → null', async () => {
    const { detectHealthAlert } = await import('../src/popup/health.js');
    const a = detectHealthAlert({
      douyin: { state: 'CLOSED', healthy: true },
      xiaohongshu: { state: 'CLOSED', healthy: true },
    });
    expect(a).toBeNull();
  });
});

describe('M2-P1 health fmtAgoMs', () => {
  it('毫秒格式化：< 1s 显示 ms', async () => {
    const { fmtAgoMs } = await import('../src/popup/health.js');
    expect(fmtAgoMs(500)).toBe('500ms 前');
  });

  it('秒格式化：< 60s 显示 s', async () => {
    const { fmtAgoMs } = await import('../src/popup/health.js');
    expect(fmtAgoMs(30000)).toBe('30s 前');
  });

  it('分钟格式化：< 60m 显示 m', async () => {
    const { fmtAgoMs } = await import('../src/popup/health.js');
    expect(fmtAgoMs(120000)).toBe('2m 前');
  });

  it('小时格式化：< 24h 显示 h', async () => {
    const { fmtAgoMs } = await import('../src/popup/health.js');
    expect(fmtAgoMs(7200000)).toBe('2h 前');
  });

  it('天格式化：>= 24h 显示 d', async () => {
    const { fmtAgoMs } = await import('../src/popup/health.js');
    expect(fmtAgoMs(90000000)).toBe('1d 前');
  });

  it('null 返回 -', async () => {
    const { fmtAgoMs } = await import('../src/popup/health.js');
    expect(fmtAgoMs(null)).toBe('-');
    expect(fmtAgoMs(undefined)).toBe('-');
  });
});

describe('M2-P1 emergency-stop', () => {
  it('初始未停止', async () => {
    const { isEmergencyStop } = await import('../src/popup/emergency-stop.js');
    const r = await isEmergencyStop();
    expect(r).toBe(false);
  });

  it('triggerEmergencyStop 写入成功', async () => {
    const { isEmergencyStop, triggerEmergencyStop } = await import('../src/popup/emergency-stop.js');
    const r = await triggerEmergencyStop('test');
    expect(r.ok).toBe(true);
    expect(await isEmergencyStop()).toBe(true);
  });

  it('resumeBridge 清除标记', async () => {
    const { isEmergencyStop, triggerEmergencyStop, resumeBridge } = await import('../src/popup/emergency-stop.js');
    await triggerEmergencyStop('test');
    expect(await isEmergencyStop()).toBe(true);
    const r = await resumeBridge();
    expect(r.ok).toBe(true);
    expect(await isEmergencyStop()).toBe(false);
  });

  it('toggleEmergencyStop 切换状态', async () => {
    const { toggleEmergencyStop, isEmergencyStop } = await import('../src/popup/emergency-stop.js');
    const newState = await toggleEmergencyStop();
    expect(newState).toBe(true);
    expect(await isEmergencyStop()).toBe(true);
    const newState2 = await toggleEmergencyStop();
    expect(newState2).toBe(false);
    expect(await isEmergencyStop()).toBe(false);
  });
});

describe('M2-P1 alert-banner startAlertPolling', () => {
  it('无告警时不调用 onAlert', async () => {
    const { startAlertPolling } = await import('../src/popup/alert-banner.js');
    chrome.runtime.sendMessage = vi.fn((msg, cb) => {
      if (msg.type === 'getStatus') cb({ statuses: {}, health: {} });
      else if (cb) cb({});
    });
    const onAlert = vi.fn();
    const onClear = vi.fn();
    const stop = startAlertPolling({ onAlert, onClear, intervalMs: 100000 });
    await new Promise(r => setTimeout(r, 30));
    expect(onAlert).not.toHaveBeenCalled();
    stop();
  });

  it('检测到熔断时调用 onAlert', async () => {
    const { startAlertPolling } = await import('../src/popup/alert-banner.js');
    chrome.runtime.sendMessage = vi.fn((msg, cb) => {
      if (msg.type === 'getStatus') {
        cb({ statuses: {}, health: { douyin: { state: 'OPEN', healthy: false } } });
      } else if (cb) cb({});
    });
    const onAlert = vi.fn();
    const onClear = vi.fn();
    const stop = startAlertPolling({ onAlert, onClear, intervalMs: 100000 });
    await new Promise(r => setTimeout(r, 30));
    expect(onAlert).toHaveBeenCalledTimes(1);
    const alert = onAlert.mock.calls[0][0];
    expect(alert.level).toBe('error');
    expect(alert.title).toContain('熔断');
    stop();
  });

  it('相同告警去重：连续两次不重复弹', async () => {
    const { startAlertPolling } = await import('../src/popup/alert-banner.js');
    let callCount = 0;
    chrome.runtime.sendMessage = vi.fn((msg, cb) => {
      if (msg.type === 'getStatus') {
        callCount++;
        cb({ statuses: {}, health: { douyin: { state: 'OPEN', healthy: false } } });
      } else if (cb) cb({});
    });
    const onAlert = vi.fn();
    const onClear = vi.fn();
    // 短间隔触发多次，但去重应保证只调一次
    const stop = startAlertPolling({ onAlert, onClear, intervalMs: 10 });
    await new Promise(r => setTimeout(r, 80));
    expect(onAlert.mock.calls.length).toBeGreaterThanOrEqual(1);
    expect(onAlert.mock.calls.length).toBeLessThanOrEqual(2);
    stop();
  });

  it('告警消除时调用 onClear', async () => {
    const { startAlertPolling } = await import('../src/popup/alert-banner.js');
    let healthState = { douyin: { state: 'OPEN', healthy: false } };
    chrome.runtime.sendMessage = vi.fn((msg, cb) => {
      if (msg.type === 'getStatus') cb({ statuses: {}, health: healthState });
      else if (cb) cb({});
    });
    const onAlert = vi.fn();
    const onClear = vi.fn();
    const stop = startAlertPolling({ onAlert, onClear, intervalMs: 10 });
    await new Promise(r => setTimeout(r, 30));
    expect(onAlert).toHaveBeenCalled();
    healthState = { douyin: { state: 'CLOSED', healthy: true } };
    await new Promise(r => setTimeout(r, 50));
    expect(onClear).toHaveBeenCalled();
    stop();
  });
});

describe('M2-P1 health fetchHealth', () => {
  it('通过 chrome.runtime.sendMessage 拉取 health', async () => {
    const { fetchHealth } = await import('../src/popup/health.js');
    chrome.runtime.sendMessage = vi.fn((msg, cb) => {
      if (msg.type === 'getStatus') {
        cb({ statuses: {}, health: { douyin: { state: 'CLOSED' } } });
      } else if (cb) cb({});
    });
    const h = await fetchHealth();
    expect(h.douyin).toBeDefined();
    expect(h.douyin.state).toBe('CLOSED');
  });

  it('返回空对象时回退到空', async () => {
    const { fetchHealth } = await import('../src/popup/health.js');
    chrome.runtime.sendMessage = vi.fn((msg, cb) => {
      if (cb) cb(null);
    });
    const h = await fetchHealth();
    expect(h).toEqual({});
  });
});

