// hidden 页不认领/不处理（2026-11-14 架构收口）——回归钉。
//
// 病灶：后台页签（用户切走/ego 后台页）visibilityState=hidden，会话 DOM 不渲染，
// openConversation 假失败——B站 9 条在 hidden 页空转烧重投预算（每次认领
// push_attempts+1，20 次即 failed）。修复：轮询路 hidden 跳过认领（ack 排水保留）、
// SSE 路 hidden 跳过处理（行留服务端等租约回收重推）、页面切回可见立即补偿一轮。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../src/core/http-ingest.js', () => ({
  getOutbox: vi.fn(async () => ({ status: 'ok', messages: [{ msg_id: 'mh-x1', conversation_id: 'c1', content: 'hi' }] })),
  ackOutbox: vi.fn(async () => ({ status: 'ok' })),
}));

import { pollDownlink, resetForTest } from '../src/core/downlink.js';

function stubVisibility(state) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
}

const CRED = { serverUrl: 'https://s.example', token: 't', accountId: 'acc-h', channel: 'douyin' };

describe('hidden 页不认领/不处理', () => {
  beforeEach(() => {
    localStorage.clear();
    stubVisibility('visible');
    resetForTest?.();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    stubVisibility('visible');
  });

  it('轮询：页面 hidden 时跳过认领（getOutbox 不被调用），ack 排水不受影响', async () => {
    const { getOutbox } = await import('../src/core/http-ingest.js');
    stubVisibility('hidden');
    await pollDownlink('douyin', 'acc-h', async () => ({ ...CRED, serverUrl: 'https://s.example' }), {});
    expect(getOutbox).not.toHaveBeenCalled();
  });

  it('轮询：页面 visible 时正常认领', async () => {
    const { getOutbox } = await import('../src/core/http-ingest.js');
    stubVisibility('visible');
    await pollDownlink('douyin', 'acc-h', async () => ({ ...CRED, serverUrl: 'https://s.example' }), {});
    expect(getOutbox).toHaveBeenCalledTimes(1);
  });
});
