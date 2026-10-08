
import { describe, it, expect } from 'vitest';
import {
  DEFAULT_USER_SERVER,
  PLATFORM_ENTRY_URLS,
  RATE_LIMIT_DEFAULTS,
  UI_DEFAULTS,
  PROTOCOL,
  SECURITY,
} from '../src/core/constants.js';

describe('DEFAULT_USER_SERVER', () => {
  it('host 必须是 localhost（dev 默认）', () => {
    expect(DEFAULT_USER_SERVER.host).toBe('localhost');
  });

  it('port 必须为 8204（与 DEVELOPMENT.md 端口表 / Dockerfile ENV 一致）', () => {
    expect(DEFAULT_USER_SERVER.port).toBe(8204);
  });

  it('baseUrl 必须由 host+port 正确拼接', () => {
    expect(DEFAULT_USER_SERVER.baseUrl).toBe(`http://${DEFAULT_USER_SERVER.host}:${DEFAULT_USER_SERVER.port}`);
  });

  it('healthPaths 必须按优先级排序：/health 优先', () => {
    expect(DEFAULT_USER_SERVER.healthPaths[0]).toBe('/health');
    expect(DEFAULT_USER_SERVER.healthPaths).toContain('/healthz');
    expect(DEFAULT_USER_SERVER.healthPaths).toContain('/readyz');
  });

  it('profile 必须为 dev', () => {
    expect(DEFAULT_USER_SERVER.profile).toBe('dev');
  });
});

describe('PLATFORM_ENTRY_URLS', () => {
  it('抖音/小红书/TikTok 三个 URL 都必须 https://', () => {
    for (const [k, url] of Object.entries(PLATFORM_ENTRY_URLS)) {
      expect(url.startsWith('https://'), `${k} 必须 https`).toBe(true);
    }
  });

  it('三个渠道名必须与 PROTOCOL.CHANNELS 严格一致', () => {
    for (const k of Object.keys(PLATFORM_ENTRY_URLS)) {
      expect(Object.values(PROTOCOL.CHANNELS)).toContain(k);
    }
  });
});

describe('RATE_LIMIT_DEFAULTS', () => {
  it('所有字段必须为正数', () => {
    for (const [k, v] of Object.entries(RATE_LIMIT_DEFAULTS)) {
      expect(typeof v, `${k} 必须为 number`).toBe('number');
      expect(v, `${k} 必须 > 0`).toBeGreaterThan(0);
    }
  });

  it('jitter 区间必须合法（min < max）', () => {
    expect(RATE_LIMIT_DEFAULTS.jitterMinMs).toBeLessThan(RATE_LIMIT_DEFAULTS.jitterMaxMs);
  });

  it('accountCapacity 必须 ≤ 后端兜底 60/min（前端更严格）', () => {
    expect(RATE_LIMIT_DEFAULTS.accountCapacity).toBeLessThanOrEqual(60);
  });

  it('minIntervalMs 必须 < jitterMaxMs（避免出现负 waitHintMs）', () => {
    expect(RATE_LIMIT_DEFAULTS.minIntervalMs).toBeLessThan(RATE_LIMIT_DEFAULTS.jitterMaxMs);
  });

  it('冻结对象，防止就地修改', () => {
    expect(Object.isFrozen(RATE_LIMIT_DEFAULTS)).toBe(true);
  });
});

describe('WS_CLIENT_DEFAULTS 已随 WS 传输一起删除', () => {
  // 扩展这条链路两侧都没有 WebSocket 了：服务端 internal/bridge 包里 pongWait/pingPeriod 零命中
  // （那个 WS handler 早随传输一起删除），扩展 src/ 里 new WebSocket 零命中。留下的只有
  // HTTP 上行/下行轮询/ack 三条加一条 SSE。（channelgw 的 /api/ws/channel 是另一条口的东西，
  // 扩展不连它。）这格守的是「别再往 DEFAULT_USER_SERVER 里塞一个指向不存在端点的字段」
  // ——历史上 wsPath:'/api/ws/bridge' 就是这么活着的，而服务端从未注册过那个路径。
  it('DEFAULT_USER_SERVER 的字段集合就是这四条通道用到的那些', () => {
    expect(Object.keys(DEFAULT_USER_SERVER).sort()).toEqual(
      ['baseUrl', 'healthPaths', 'host', 'port', 'profile'].sort()
    );
  });

  it('模块不再导出 WS_CLIENT_DEFAULTS', async () => {
    const mod = await import('../src/core/constants.js');
    expect(mod.WS_CLIENT_DEFAULTS).toBeUndefined();
  });
});

describe('UI_DEFAULTS', () => {
  it('healthCheckTimeoutMs 必须 1-10s（不能太短也不能太久）', () => {
    expect(UI_DEFAULTS.healthCheckTimeoutMs).toBeGreaterThanOrEqual(1000);
    expect(UI_DEFAULTS.healthCheckTimeoutMs).toBeLessThanOrEqual(10_000);
  });
});

describe('PROTOCOL', () => {
  it('CHANNELS 值：2026-08-05 渠道编码统一后全部为全名（无 _web 后缀）', () => {
    // 2026-08-05 渠道编码统一：bridge 渠道名 = 平台全名（xiaohongshu/douyin/kuaishou/xianyu/tiktok），
    // 与后端 model.Channel*、SQL 数据、channel_agent_bindings 完全一致。
    const expected = new Set(['xiaohongshu', 'douyin', 'kuaishou', 'xianyu', 'tiktok']);
    for (const v of Object.values(PROTOCOL.CHANNELS)) {
      expect(expected.has(v)).toBe(true);
    }
  });

  it('FRAME 名称必须与 user-server/internal/bridge/frames.go 严格一致', () => {
    expect(PROTOCOL.FRAME).toEqual({
      REGISTER: 'register',
      INBOUND: 'inbound_message',
      HISTORY: 'history',
      OUTBOUND: 'outbound_reply',
      PONG: 'pong',
      PING: 'ping',
      ACK: 'ack',
      ERROR: 'error',
    });
  });

  it('冻结对象', () => {
    expect(Object.isFrozen(PROTOCOL)).toBe(true);
  });
});

describe('SECURITY', () => {
  it('maxReplyContentBytes 必须与服务端 handler.go maxReplyContentBytes 一致', () => {
    expect(SECURITY.maxReplyContentBytes).toBe(4 * 1024);
  });

  it('logMaskMaxChars 必须为正', () => {
    expect(SECURITY.logMaskMaxChars).toBeGreaterThan(0);
    expect(SECURITY.logMaskMaxChars).toBeLessThanOrEqual(100);
  });
});

describe('DEFAULTS 文档源完整性', () => {
  it('每个顶层常量都必须在 DEFAULTS.md 出现（人工检查）', () => {
    // 此项测试为占位提醒：每次新增顶层常量请同步 DEFAULTS.md
    const expected = [
      'DEFAULT_USER_SERVER',
      'PLATFORM_ENTRY_URLS',
      'RATE_LIMIT_DEFAULTS',
      'UI_DEFAULTS',
      'PROTOCOL',
      'SECURITY',
    ];
    expect(typeof DEFAULT_USER_SERVER).toBe('object');
    expect(typeof PLATFORM_ENTRY_URLS).toBe('object');
    expect(typeof RATE_LIMIT_DEFAULTS).toBe('object');
    expect(typeof UI_DEFAULTS).toBe('object');
    expect(typeof PROTOCOL).toBe('object');
    expect(typeof SECURITY).toBe('object');
    expect(expected.length).toBe(6);
  });
});

