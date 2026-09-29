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
 * getServerCapabilities 查询服务端下行能力。
 * 请求 GET /api/bridge/capabilities，返回 { sse_enabled, poll_interval_ms }。
 * 请求失败时返回保守的 fallback（sse_enabled: false）。
 *
 * @param {object} [cfg] 可选配置 { serverUrl, token }
 * @returns {Promise<{sse_enabled: boolean, poll_interval_ms: number}>}
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
      return { sse_enabled: false, poll_interval_ms: BRIDGE_THREE_CHANNEL.outboxPollIntervalMs };
    }
    const data = await resp.json();
    return {
      sse_enabled: !!(data && data.sse_enabled),
      poll_interval_ms: (data && data.poll_interval_ms) || BRIDGE_THREE_CHANNEL.outboxPollIntervalMs,
    };
  } catch (err) {
    log.warn('capabilities 查询失败，使用 fallback', err && err.message);
    return { sse_enabled: false, poll_interval_ms: BRIDGE_THREE_CHANNEL.outboxPollIntervalMs };
  }
}

export class PollingLoop {
  constructor({ channels, getConfig, getMeta, getAdapter, uplinks, preferSSE }) {
    this.channels = channels || [];
    this.getConfig = getConfig;
    this.getMeta = getMeta;
    this.getAdapter = getAdapter;
    this.uplinks = uplinks || new Map();
    this._running = false;
    this._downlinkTimer = null;
    this._downlinkInFlight = false;
    this._preferSSE = preferSSE !== false; // 默认尝试 SSE
    this._sseCleanups = new Map(); // channel -> cleanup function
    this._sseActive = false;
  }

  async start() {
    if (this._running) return;
    this._running = true;

    // 初始化下发缓存
    initDownlink(this.channels);

    // 尝试 SSE 模式
    if (this._preferSSE) {
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
          sendOutboundTimeoutMs: BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs,
          outboxBatchSize: BRIDGE_THREE_CHANNEL.outboxBatchSize,
        });
      } catch (e) {
        log.error(`下发轮询 [${ch}] 异常`, e);
      }
    }
  }
}
