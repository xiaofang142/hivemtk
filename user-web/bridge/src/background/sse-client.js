// background/sse-client.js
// Service Worker 中的 SSE 客户端：
// 1. 监听 chrome.alarms keepalive（每 15s 唤醒 Worker）
// 2. 经 core/sse-fetch-client（fetch 流式，X-Channel-Identify-Code 头携带识别码）连接 /api/channelbus/outbox/sse
// 3. 按 (channel, account_id) 路由消息到对应标签页的 Content Script
// 4. 断线重连带 Last-Event-ID
// 5. 持久化 Last-Event-ID 到 chrome.storage.local

import { createLogger } from '../core/logger.js';
import { DEFAULT_USER_SERVER } from '../core/constants.js';
import { connectSSE, isConnected, stopSSE } from '../core/sse-fetch-client.js';
// 出站唤醒：后台收到 outbox 投递信号后立即拉取（025 出站唤醒切片）。
import './outbox-wake.js';

const log = createLogger('sse', 'bg');

const SSE_KEEPALIVE_ALARM = 'bridge_sse_keepalive';
const SSE_RECONNECT_DELAY_MS = 3000;

// 全局状态
let sseConnections = new Map(); // key = "channel:account_id" -> 连接状态 { stopped, reconnectTimer, close }
let lastEventIDs = new Map();   // key = "channel:account_id" -> last event ID
let tabRegistry = new Map();    // key = "channel:account_id" -> Set of tabIds

// ---- 工具函数 ----

function getBaseUrl() {
  return new Promise((resolve) => {
    try {
      chrome.storage.local.get('bridgeConfig', (res) => {
        const cfg = res && res.bridgeConfig ? res.bridgeConfig : {};
        resolve(cfg.serverUrl || DEFAULT_USER_SERVER.baseUrl);
      });
    } catch (_) {
      resolve(DEFAULT_USER_SERVER.baseUrl);
    }
  });
}

function getAuthToken() {
  return new Promise((resolve) => {
    try {
      chrome.storage.local.get('bridgeConfig', (res) => {
        const cfg = res && res.bridgeConfig ? res.bridgeConfig : {};
        resolve(cfg.token || '');
      });
    } catch (_) {
      resolve('');
    }
  });
}

// 租户短码：SaaS 模式下服务端靠它（或账号行自证）定归属，见
// user-server/internal/bridge/tenant_binding.go:47-56。离线单机留空。
// T7：原 appendTenantCode（EventSource 路径手工拼 query）已由 core/sse-fetch-client
// connectSSE 的 tenantCode 选项承担——同一 TENANT_CODE.QUERY_PARAM 键名、空值跳过、
// searchParams.set 编码，行为不变。
function getTenantCode() {
  return new Promise((resolve) => {
    try {
      chrome.storage.local.get('bridgeConfig', (res) => {
        const cfg = res && res.bridgeConfig ? res.bridgeConfig : {};
        resolve(typeof cfg.tenantCode === 'string' ? cfg.tenantCode.trim() : '');
      });
    } catch (_) {
      resolve('');
    }
  });
}

// ---- Tab 注册/注销（与 background/index.js 的消息监听器共存）----

if (chrome && chrome.runtime && chrome.runtime.onMessage) {
  chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (!msg || !msg.type) return false;

  if (msg.type === 'BRIDGE_REGISTER_TAB') {
    const key = `${msg.channel}:${msg.accountId}`;
    if (!tabRegistry.has(key)) tabRegistry.set(key, new Set());
    tabRegistry.get(key).add(sender.tab.id);
    log.info(`SSE tab 注册: ${key} -> tab ${sender.tab.id}, 总 ${tabRegistry.get(key).size} 个 tab`);
    // 触发 SSE 连接建立
    ensureSSEConnection(msg.channel, msg.accountId).catch((err) => {
      log.error('ensureSSEConnection 失败', err);
    });
    sendResponse?.({ ok: true });
    return false;
  }

  if (msg.type === 'BRIDGE_UNREGISTER_TAB') {
    const key = `${msg.channel}:${msg.accountId}`;
    if (tabRegistry.has(key)) {
      tabRegistry.get(key).delete(sender.tab.id);
      if (tabRegistry.get(key).size === 0) {
        tabRegistry.delete(key);
        // 无 tab 注册时关闭 SSE 连接
        const st = sseConnections.get(key);
        if (st) {
          st.close();
          log.info(`SSE 连接已关闭（无 tab 注册）: ${key}`);
        }
      }
    }
    log.info(`SSE tab 注销: ${key} -> tab ${sender.tab.id}`);
    sendResponse?.({ ok: true });
    return false;
  }

  // M-1（2026-11-11）：BRIDGE_SSE_ACK 死路径已删除（全仓无发送方；下发 ack 的活链路
  // 是 content 侧 downlink → http-ingest.ackOutbox，见 test/downlink-r23-sse-ack-drain.test.js）。

  return false;
});
} else {
  log.warn('chrome.runtime.onMessage 不可用，跳过消息监听器注册');
}

// ---- SSE 连接管理（T7，2026-11-17：fetch 流式替代 EventSource）----
//
// 旧实现用 EventSource，不支持自定义 Header，只能把凭证拼进 URL 查询参数（已退役）。
// 现在统一走 core/sse-fetch-client.js 的 connectSSE（downlink.js 下行主通道同款实现）：
// 识别码经 X-Channel-Identify-Code 头发送，URL 上只剩 channel/account_id/last_event_id/
// tenant_code，不再有任何凭证参数。
// 对外语义保持不变：onmessage（JSON 解析后路由到 tab）、onerror（日志 + 3s 重连）、
// Last-Event-ID 持久化（内存缓存 + chrome.storage）、tenant_code 附加。

async function ensureSSEConnection(channel, accountId) {
  const key = `${channel}:${accountId}`;
  const existing = sseConnections.get(key);
  if (existing && !existing.stopped) return existing;

  // 读取保存的 Last-Event-ID
  let lastEventID = '';
  try {
    const stored = await chrome.storage.local.get(`last_event_${key}`);
    lastEventID = stored[`last_event_${key}`] || '';
  } catch (_) {}

  const baseUrl = await getBaseUrl();
  const token = await getAuthToken();
  const tenantCode = await getTenantCode();

  log.info(`SSE 连接中: ${key}, lastEventID=${lastEventID || '(none)'}`);

  const state = { stopped: false, reconnectTimer: null, close: null };
  sseConnections.set(key, state);

  // 关闭/清理（等价旧实现的 es.close() + sseConnections.delete + 内存缓存清理）
  const close = () => {
    state.stopped = true;
    if (state.reconnectTimer) {
      clearTimeout(state.reconnectTimer);
      state.reconnectTimer = null;
    }
    try { stopSSE(channel, accountId); } catch (_) {}
    if (sseConnections.get(key) === state) sseConnections.delete(key);
    lastEventIDs.delete(key);
  };
  state.close = close;

  // 断线重连：等价旧实现 error 监听器里的 setTimeout(ensureSSEConnection, 3s)。
  // 到点时清掉本轮占位再重建——ensureSSEConnection 会重新读 storage 取最新 Last-Event-ID，
  // 与旧实现的每次重连都走一遍完整建连路径一致。
  const scheduleReconnect = () => {
    if (state.reconnectTimer) return;
    state.reconnectTimer = setTimeout(() => {
      state.reconnectTimer = null;
      if (state.stopped) return;
      state.stopped = true;
      if (sseConnections.get(key) === state) sseConnections.delete(key);
      ensureSSEConnection(channel, accountId).catch((err) => {
        log.error('SSE 重连失败', err);
      });
    }, SSE_RECONNECT_DELAY_MS);
  };

  // 一轮连接尝试。connectSSE 在流结束（服务端关流）时 resolve、出错时先调 onError 再 throw，
  // 两条路都触发重连；主动关闭（close/stopSSE）以 AbortError 收尾，靠 state.stopped 区分。
  (async () => {
    if (state.stopped) return;
    try {
      const stop = await connectSSE(channel, accountId, {
        serverUrl: baseUrl,
        token,
        tenantCode,
        lastEventId: lastEventID,
        onMessage: (data) => {
          // 等价旧实现 message 监听器的 JSON.parse + dispatchToTabs（解析失败由
          // connectSSE 内部按非 JSON 事件降级为 debug 日志，不会打断连接）
          dispatchToTabs(channel, accountId, data);
        },
        onLastEventID: (id) => {
          // 等价旧实现的 event.lastEventId 分支：内存缓存 + chrome.storage 持久化
          lastEventIDs.set(key, id);
          chrome.storage.local.set({ [`last_event_${key}`]: id }).catch(() => {});
        },
      });
      void stop;
      if (state.stopped) return;
      log.warn(`SSE 流结束: ${key}, 将在 ${SSE_RECONNECT_DELAY_MS}ms 后重连`);
      scheduleReconnect();
    } catch (err) {
      if (state.stopped) return;
      log.warn(`SSE 连接错误: ${key}, 将在 ${SSE_RECONNECT_DELAY_MS}ms 后重连`, err && err.message);
      // 清理 lastEventID 内存缓存（与旧实现 error 监听器一致；chrome.storage 里的
      // 持久值保留，重连时用它续传）
      lastEventIDs.delete(key);
      scheduleReconnect();
    }
  })();

  return state;
}

function dispatchToTabs(channel, accountId, data) {
  const key = `${channel}:${accountId}`;
  const tabIds = tabRegistry.get(key);
  if (!tabIds || tabIds.size === 0) {
    log.warn(`SSE 无注册 tab: ${key}`);
    return;
  }
  const message = {
    type: 'SSE_OUTBOUND',
    data: data,
    channel: channel,
    accountId: accountId,
  };
  for (const tabId of tabIds) {
    try {
      chrome.tabs.sendMessage(tabId, message, () => {
        const err = chrome.runtime.lastError;
        if (err) {
          // tab 可能已关闭，从注册表移除
          log.warn(`SSE 发送到 tab ${tabId} 失败: ${err.message}`);
          if (tabRegistry.has(key)) {
            tabRegistry.get(key).delete(tabId);
            if (tabRegistry.get(key).size === 0) {
              tabRegistry.delete(key);
              const st = sseConnections.get(key);
              if (st) {
                st.close();
              }
            }
          }
        }
      });
    } catch (_) {}
  }
}

// ---- Keepalive（防 MV3 Worker 被 Chrome 杀掉）----

if (chrome && chrome.alarms && chrome.alarms.onAlarm) {
  chrome.alarms.onAlarm.addListener((alarm) => {
    if (alarm.name === SSE_KEEPALIVE_ALARM) {
      log.info('SSE keepalive alarm 触发');
      // 唤醒 Worker 并确保 SSE 连接存活
      // T7：EventSource.readyState 判活改为 sse-fetch-client 的 isConnected；
      // 若本轮连接仍在 3s 重连等待窗口里，ensureSSEConnection 会命中占位直接返回。
      for (const [key, st] of sseConnections) {
        const [channel, accountId] = key.split(':');
        if (!st.stopped && !isConnected(channel, accountId)) {
          log.warn(`SSE 连接已关闭，尝试重连: ${key}`);
          ensureSSEConnection(channel, accountId).catch(() => {});
        }
      }
    }
  });
} else {
  log.warn('chrome.alarms 不可用，跳过 keepalive alarm 注册');
}

// ---- 启动时恢复 ----

if (chrome && chrome.runtime && chrome.runtime.onStartup) {
  chrome.runtime.onStartup.addListener(() => {
    log.info('SSE onStartup: 创建 keepalive alarm');
    try {
      if (chrome.alarms) {
        chrome.alarms.create(SSE_KEEPALIVE_ALARM, { periodInMinutes: 1 });
      }
    } catch (_) {}

    // 从 chrome.storage 恢复活跃的账号列表
    // 实际部署时可以从 accounts 列表恢复连接
    if (chrome.storage && chrome.storage.local) {
      chrome.storage.local.get(['bridgeConfig'], (res) => {
        const cfg = res && res.bridgeConfig ? res.bridgeConfig : {};
        const accounts = cfg.accounts || {};
        for (const [_channel, acctMap] of Object.entries(accounts)) {
          if (!acctMap || typeof acctMap !== 'object') continue;
          for (const [_accountId, acctCfg] of Object.entries(acctMap)) {
            if (acctCfg && acctCfg.enabled === false) continue;
          }
        }
      });
    }
  });
}

// ---- 导出（供测试使用）----

export function _getSSEState() {
  return {
    connections: sseConnections.size,
    tabs: Object.fromEntries(
      Array.from(tabRegistry.entries()).map(([k, v]) => [k, v.size])
    ),
    lastEventIDs: Object.fromEntries(lastEventIDs),
  };
}

export function _resetSSEState() {
  for (const [, st] of sseConnections) {
    try { st.close(); } catch (_) {}
  }
  sseConnections.clear();
  lastEventIDs.clear();
  tabRegistry.clear();
}
