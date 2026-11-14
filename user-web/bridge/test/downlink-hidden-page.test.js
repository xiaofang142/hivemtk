// hidden 页认领语义（2026-11-14 18:30 对抗审查回归钉）。
//
// 事故复盘：曾错误地在 pollDownlink/SSE 加 hidden 跳过——本扩展的常态运行形态就是
// 后台/不可见页（ego 隔离窗口恒 hidden、用户切走的页签），6 小时 14 条 delivered
// 全部发生在 hidden 页上（CDP 拟人键入在 hidden 页有效）。hidden 跳过 = 全渠道
// 下发静默死亡（18:05-18:30 生产回归实证）。本文件钉住正确语义：
//   ★ hidden 页必须照常认领与处理下发——hidden 不是跳过理由；
//   ★ visible 页同样照常（行为不随可见性翻转）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const getOutboxMock = vi.fn(async () => ({ status: 'ok', messages: [] }));
const ackOutboxMock = vi.fn(async () => ({ status: 'ok' }));

vi.mock('../src/core/http-ingest.js', () => ({
  getOutbox: (...a) => getOutboxMock(...a),
  ackOutbox: (...a) => ackOutboxMock(...a),
}));

import { pollDownlink } from '../src/core/downlink.js';

function stubVisibility(state) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
}

const CFG = async () => ({ serverUrl: 'https://s.example', token: 't', accountId: 'acc-h', tenantCode: '' });

describe('hidden 页认领语义（对抗审查回归钉）', () => {
  beforeEach(() => {
    localStorage.clear();
    getOutboxMock.mockClear();
    ackOutboxMock.mockClear();
    stubVisibility('visible');
  });
  afterEach(() => {
    vi.restoreAllMocks();
    stubVisibility('visible');
  });

  it('★ hidden 页必须照常认领（ego/后台页是常态运行形态）', async () => {
    stubVisibility('hidden');
    await pollDownlink('douyin', 'acc-h', CFG, {});
    expect(getOutboxMock).toHaveBeenCalledTimes(1);
  });

  it('visible 页照常认领（行为不随可见性翻转）', async () => {
    await pollDownlink('douyin', 'acc-h', CFG, {});
    expect(getOutboxMock).toHaveBeenCalledTimes(1);
  });
});
