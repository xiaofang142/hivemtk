// M-6（2026-11-11）：getServerCapabilities 的 followUser 配置解析。
// 验收锚点：follow_user_enabled 默认关（字段缺失/响应失败/非 OK 一律 false），
// 渠道清单容错解析（JSON 字符串/数组/缺省）。
import { describe, it, expect, vi, beforeEach } from 'vitest';

let fetchMock = null;

import { getServerCapabilities } from '../src/core/polling-loop.js';
import { BRIDGE_THREE_CHANNEL } from '../src/core/constants.js';

const cfg = { serverUrl: 'http://localhost:8204', token: 'tkn-1' };

beforeEach(() => {
  fetchMock = null;
  vi.stubGlobal('fetch', vi.fn((url, opts) => Promise.resolve(fetchMock(url, opts))));
});

describe('polling-loop / capabilities follow_user 解析', () => {
  it('服务端全字段下发 → 原样解析（enabled=true + 渠道清单）', async () => {
    fetchMock = () => new Response(JSON.stringify({
      sse_enabled: false,
      poll_interval_ms: 1500,
      patrol_unread_first_channels: ['douyin'],
      follow_user_enabled: true,
      follow_user_channels: ['douyin', 'tiktok', 'bilibili'],
    }), { status: 200 });
    const caps = await getServerCapabilities(cfg);
    expect(caps.follow_user_enabled).toBe(true);
    expect(caps.follow_user_channels).toEqual(['douyin', 'tiktok', 'bilibili']);
  });

  it('字段缺失（旧服务端）→ 默认关（follow_user_enabled=false + 空清单）', async () => {
    fetchMock = () => new Response(JSON.stringify({ sse_enabled: false, poll_interval_ms: 1500 }), { status: 200 });
    const caps = await getServerCapabilities(cfg);
    expect(caps.follow_user_enabled).toBe(false);
    expect(caps.follow_user_channels).toEqual([]);
  });

  it('响应非 OK / fetch 异常 → fallback 默认关（fail-safe）', async () => {
    fetchMock = () => new Response('err', { status: 500 });
    let caps = await getServerCapabilities(cfg);
    expect(caps.follow_user_enabled).toBe(false);
    expect(caps.follow_user_channels).toEqual([]);
    expect(caps.poll_interval_ms).toBe(BRIDGE_THREE_CHANNEL.outboxPollIntervalMs);

    fetchMock = () => Promise.reject(new Error('network down'));
    caps = await getServerCapabilities(cfg);
    expect(caps.follow_user_enabled).toBe(false);
    expect(caps.follow_user_channels).toEqual([]);
  });

  it('follow_user_channels 为 JSON 字符串（服务端实际形状）→ 容错解析', async () => {
    fetchMock = () => new Response(JSON.stringify({
      follow_user_enabled: true,
      follow_user_channels: '["douyin"]',
    }), { status: 200 });
    const caps = await getServerCapabilities(cfg);
    expect(caps.follow_user_channels).toEqual(['douyin']);
  });

  it('enabled=true 但清单非数组 → 清单空（闸3 fail-closed 拒绝执行）', async () => {
    fetchMock = () => new Response(JSON.stringify({
      follow_user_enabled: true,
      follow_user_channels: 'garbage-not-json',
    }), { status: 200 });
    const caps = await getServerCapabilities(cfg);
    expect(caps.follow_user_enabled).toBe(true);
    expect(caps.follow_user_channels).toEqual([]);
  });
});
