/**
 * PollingLoop —— 下发轮询调度器
 *
 * 巡检（遍历会话列表）由 ChannelAdapter.startPatrol 自循环负责
 * （PATROL_DEFAULTS 风控节奏：30s 间隔、maxPerRound=6、3-5s 切换、会话冷却 120s）。
 * 本类只负责 downlink outbox 下发轮询，避免双套巡检导致 0ms 空转。
 *
 * SSE 模式：通过 getServerCapabilities() 查询服务端是否支持 SSE，
 * 若支持则优先使用 startSSEDelivery()，否则 fallback 到轮询。
 */
import { createLogger } from './logger.js';
import { BRIDGE_THREE_CHANNEL, DEFAULT_USER_SERVER } from './constants.js';
import { pollDownlink, initDownlink, startSSEDelivery } from './downlink.js';

const log = createLogger('bridge', 'bg');

// ---- 服务端能力查询 ----

/**
 * 解析 capabilities 里的「巡检未读优先渠道清单」。
 * 服务端（参数中心 bridge/patrol_unread_first_channels）下发 JSON 字符串数组；
 * 字段缺失（旧服务端）/非数组/解析失败 → 空数组 = 扩展侧开关全关（现行顺序遍历）。
 * 兼容两种形状：直接数组（容错）与 JSON 字符串（服务端实际形状）。
 */
function parseUnreadFirstChannels(data) {
  try {
    let raw = data ? data.patrol_unread_first_channels : null;
    if (typeof raw === 'string') {
      try { raw = JSON.parse(raw); } catch (_) { return []; }
    }
    if (!Array.isArray(raw)) return [];
    return raw.filter((ch) => typeof ch === 'string' && ch);
  } catch (_) { return []; }
}

/**
 * M-6：解析 capabilities 里的 followUser 自动关注配置。
 *   follow_user_enabled  bool（参数中心 bridge/follow_user_enabled，默认 false）
 *   follow_user_channels JSON 字符串数组（bridge/follow_user_channels）
 * 字段缺失/解析失败 = 默认关（扩展侧永不执行关注动作）。
 */
function parseFollowUserCapabilities(data) {
  const fallback = { follow_user_enabled: false, follow_user_channels: [] };
  try {
    let channels = data ? data.follow_user_channels : null;
    if (typeof channels === 'string') {
      try { channels = JSON.parse(channels); } catch (_) { channels = []; }
    }
    if (!Array.isArray(channels)) channels = [];
    return {
      follow_user_enabled: !!(data && data.follow_user_enabled === true),
      follow_user_channels: channels.filter((ch) => typeof ch === 'string' && ch),
    };
  } catch (_) { return fallback; }
}

/**
 * getServerCapabilities 查询服务端下行能力。
 * 请求 GET /api/bridge/capabilities，
 * 返回 { sse_enabled, poll_interval_ms, patrol_unread_first_channels,
 *        follow_user_enabled, follow_user_channels }（M-6）。
 * 请求失败时返回保守的 fallback（sse_enabled: false；未读优先清单 = 空 = 开关关；
 * follow_user_enabled = false = 关注动作永不执行）。
 *
 * @param {object} [cfg] 可选配置 { serverUrl, token }
 * @returns {Promise<{sse_enabled: boolean, poll_interval_ms: number, patrol_unread_first_channels: string[], follow_user_enabled: boolean, follow_user_channels: string[]}>}
 */
export async function getServerCapabilities(cfg) {
  const baseUrl = (cfg && cfg.serverUrl) || DEFAULT_USER_SERVER.baseUrl;
  const url = `${baseUrl}/api/bridge/capabilities`;
  const timeoutMs = 5000;

  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    const headers = { 'Content-Type': 'application/json' };
    if (cfg && cfg.token && cfg.token.trim()) {
      // /api/bridge/capabilities 也在 BridgeIngressGuard 之后：只发 Bearer 会 401，
      // 然后这里按"服务端不支持 SSE"降级 → 用户永远拿不到长连接下行。
      const t = cfg.token.trim();
      headers['X-Bridge-Token'] = t;
      headers['Authorization'] = `Bearer ${t}`;
    }
    const resp = await fetch(url, {
      method: 'GET',
      headers,
      signal: controller.signal,
    });
    clearTimeout(timer);

    if (!resp.ok) {
      log.warn(`capabilities 响应非 OK: ${resp.status}`);
      return {
        sse_enabled: false,
        poll_interval_ms: BRIDGE_THREE_CHANNEL.outboxPollIntervalMs,
        patrol_unread_first_channels: [],
        follow_user_enabled: false,
        follow_user_channels: [],
      };
    }
    const data = await resp.json();
    return {
      sse_enabled: !!(data && data.sse_enabled),
      poll_interval_ms: (data && data.poll_interval_ms) || BRIDGE_THREE_CHANNEL.outboxPollIntervalMs,
      // M-5：参数中心下发的未读优先巡检渠道清单（缺失/解析失败 = 空 = 现行行为）
      patrol_unread_first_channels: parseUnreadFirstChannels(data),
      // M-6：followUser 自动关注配置（缺失/解析失败 = 默认关）
      ...parseFollowUserCapabilities(data),
    };
  } catch (err) {
    log.warn('capabilities 查询失败，使用 fallback', err && err.message);
    return {
      sse_enabled: false,
      poll_interval_ms: BRIDGE_THREE_CHANNEL.outboxPollIntervalMs,
      patrol_unread_first_channels: [],
      follow_user_enabled: false,
      follow_user_channels: [],
    };
  }
}

export class PollingLoop {
  constructor({ channels, getConfig, getMeta, getAdapter, uplinks, preferSSE, pageContext }) {
    this.channels = channels || [];
    this.getConfig = getConfig;
    this.getMeta = getMeta;
    this.getAdapter = getAdapter;
    this.uplinks = uplinks || new Map();
    this._running = false;
    this._downlinkTimer = null;
    this._downlinkInFlight = false;
    this._preferSSE = preferSSE !== false; // 默认尝试 SSE
    // pageContext=true：content script 显式声明「我运行在页面源下」，SSE 会被
    // 生产 CORS 拦截，强制轮询模式（getOutbox 走 bridgeFetch 已免 CORS）。
    // 未声明（扩展页 popup/SW）= 保留 SSE 语义；测试桩不传 = 默认 SSE。
    this._pageContext = pageContext === true;
    this._sseCleanups = new Map(); // channel -> cleanup function
    this._sseActive = false;
  }

  // M-6：follow_user 动作行执行器（adapter.followUser 统一开关/风控/冷却）。
  // adapter 缺方法（老渠道/测试桩）= 无执行器，动作行留 pending（与未知行同形）。
  _followActionFor(adapter) {
    if (adapter && typeof adapter.followUser === 'function') {
      return (convId) => adapter.followUser({ conversationId: convId });
    }
    return undefined;
  }

  async start() {
    if (this._running) return;
    this._running = true;

    // visible 恢复补偿（2026-11-14，配套 downlink 的 hidden 页跳过认领）：页面切回
    // 可见时立即补一轮下发，不等下一个轮询周期——hidden 期间挂起的行最快路径接手。
    // SSE 模式同样受益（SSE 推送在 hidden 期间被跳过，挂起行靠此补偿 + 服务端重推）。
    if (typeof document !== 'undefined' && document.addEventListener) {
      this._visibilityHandler = () => {
        if (document.visibilityState === 'visible') this._downlinkSafe();
      };
      document.addEventListener('visibilitychange', this._visibilityHandler);
    }

    // 初始化下发缓存
    initDownlink(this.channels);

    // 尝试 SSE 模式
    // 页面上下文（content script 运行在页面源下）直连 SSE 会被生产 CORS 拦截：
    // connectSSE 的 fetch 跑在 douyin/weibo 等页面源，workapi 不放行这些源 →
    // 出站全部卡 inflight/20 次后 exhausted（2026-11-12 生产实证）。页面上下文
    // 由构造方显式声明 pageContext:true 强制轮询（getOutbox 走 bridgeFetch 已免
    // CORS）；扩展页（popup/SW）不受 CORS 限制，未声明时保留 SSE 语义。
    if (this._preferSSE && !this._pageContext) {
      const cfg = await this.getConfig();
      const capabilities = await getServerCapabilities(cfg);

      if (capabilities.sse_enabled) {
        log.info('服务端支持 SSE，启用 SSE 模式下行');
        this._sseActive = true;
        await this._startSSEMode(cfg);
        return;
      } else {
        log.info('服务端不支持 SSE，回退到轮询模式');
      }
    }

    // 轮询模式（fallback）
    this._downlinkTimer = setInterval(
      () => this._downlinkSafe(),
      BRIDGE_THREE_CHANNEL.outboxPollIntervalMs
    );
    log.info(`下发轮询已启动：每 ${BRIDGE_THREE_CHANNEL.outboxPollIntervalMs}ms`);
  }

  async _startSSEMode(cfg) {
    const meta = this.getMeta ? this.getMeta() : {};
    const accountId = (meta && meta.accountId) || (cfg && cfg.active && cfg.active.accountId) || 'default';

    for (const ch of this.channels) {
      try {
        const adapter = this.getAdapter ? this.getAdapter(ch) : null;
        const sendOutbound = adapter && typeof adapter.sendOutbound === 'function'
          ? (text, convId, opts) => adapter.sendOutbound(text, convId, opts)
          : undefined;

        // 通过 startSSEDelivery 启动 SSE 下行
        const cleanup = await startSSEDelivery(ch, accountId, {
          sendOutbound,
          followAction: this._followActionFor(adapter),
          onMessage: (msg) => {
            log.info(`SSE 下行成功: ${ch}`, {
              msgId: msg.msgId,
              convId: msg.conversationId,
              len: msg.content ? msg.content.length : 0,
            });
          },
          onError: (err) => {
            log.error(`SSE 下行错误: ${ch}`, err);
          },
          onDuplicate: (msgId) => {
            log.debug(`SSE 下行重复消息已跳过: ${ch}:${msgId}`);
          },
        });
        this._sseCleanups.set(ch, cleanup);
      } catch (err) {
        log.error(`SSE 启动失败 [${ch}]，将 fallback 到轮询`, err);
        // 单渠道 SSE 启动失败不阻塞其他渠道
      }
    }

    // 如果所有渠道 SSE 都失败，回退到轮询
    if (this._sseCleanups.size === 0) {
      log.warn('所有渠道 SSE 均启动失败，回退到轮询模式');
      this._sseActive = false;
      this._downlinkTimer = setInterval(
        () => this._downlinkSafe(),
        BRIDGE_THREE_CHANNEL.outboxPollIntervalMs
      );
    } else {
      log.info(`SSE 下行已启动: ${this._sseCleanups.size} 个 channel`);
    }
  }

  stop() {
    this._running = false;
    if (this._visibilityHandler && typeof document !== 'undefined' && document.removeEventListener) {
      document.removeEventListener('visibilitychange', this._visibilityHandler);
      this._visibilityHandler = null;
    }

    // 清理 SSE
    for (const [ch, cleanup] of this._sseCleanups) {
      try {
        cleanup();
      } catch (err) {
        log.warn(`SSE cleanup [${ch}] 失败`, err);
      }
    }
    this._sseCleanups.clear();
    this._sseActive = false;

    // 清理轮询
    if (this._downlinkTimer) {
      clearInterval(this._downlinkTimer);
      this._downlinkTimer = null;
    }
    log.info('下发调度已停止');
  }

  async _downlinkSafe() {
    if (!this._running || this._downlinkInFlight) return;
    this._downlinkInFlight = true;
    try {
      await this._downlink();
    } catch (e) {
      log.error('下发轮询失败', e);
    } finally {
      this._downlinkInFlight = false;
    }
  }

  async _downlink() {
    const cfg = await this.getConfig();
    if (!cfg) return;
    const meta = this.getMeta ? this.getMeta() : {};
    const accountId = (meta && meta.accountId) || (cfg && cfg.active && cfg.active.accountId) || 'default';
    for (const ch of this.channels) {
      try {
        const adapter = this.getAdapter ? this.getAdapter(ch) : null;
        const sendOutbound = adapter && typeof adapter.sendOutbound === 'function'
          ? (text, convId, opts) => adapter.sendOutbound(text, convId, opts)
          : undefined;
        await pollDownlink(ch, accountId, () => this.getConfig(), {
          sendOutbound,
          followAction: this._followActionFor(adapter),
          sendOutboundTimeoutMs: BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs,
          outboxBatchSize: BRIDGE_THREE_CHANNEL.outboxBatchSize,
        });
      } catch (e) {
        log.error(`下发轮询 [${ch}] 异常`, e);
      }
    }
  }
}
