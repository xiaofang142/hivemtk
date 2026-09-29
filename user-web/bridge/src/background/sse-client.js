// background/sse-client.js
// Service Worker 中的 SSE 客户端：
// 1. 监听 chrome.alarms keepalive（每 15s 唤醒 Worker）
// 2. 建立 EventSource 连接到 /api/bridge/outbox/sse
// 3. 按 (channel, account_id) 路由消息到对应标签页的 Content Script
// 4. 断线重连带 Last-Event-ID
// 5. 持久化 Last-Event-ID 到 chrome.storage.local

import { createLogger } from '../core/logger.js';
import { DEFAULT_USER_SERVER } from '../core/constants.js';

const log = createLogger('sse', 'bg');

const SSE_KEEPALIVE_ALARM = 'bridge_sse_keepalive';
const SSE_RECONNECT_DELAY_MS = 3000;

// 全局状态
let sseConnections = new Map(); // key = "channel:account_id" -> EventSource
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
        const es = sseConnections.get(key);
        if (es) {
          es.close();
          sseConnections.delete(key);
          log.info(`SSE 连接已关闭（无 tab 注册）: ${key}`);
        }
      }
    }
    log.info(`SSE tab 注销: ${key} -> tab ${sender.tab.id}`);
    sendResponse?.({ ok: true });
    return false;
  }

  if (msg.type === 'BRIDGE_SSE_ACK') {
    // Content Script 确认消息已送达
    getBaseUrl().then((baseUrl) => {
      const url = `${baseUrl}/api/bridge/outbox/ack?channel=${encodeURIComponent(msg.channel)}&account_id=${encodeURIComponent(msg.accountId)}`;
      const token = getAuthToken();
      return Promise.all([url, token]);
    }).then(([url, token]) => {
      const headers = { 'Content-Type': 'application/json' };
      const t = (token || '').trim();
      if (t) {
        headers['X-Bridge-Token'] = t;
        headers['Authorization'] = `Bearer ${t}`;
      }
      return fetch(url, {
        method: 'POST',
        headers,
        body: JSON.stringify({ v: 2, msg_ids: msg.msgIds, status: 'delivered' }),
      });
    }).then(() => {
      log.info(`SSE ack 成功: ${msg.channel}`, { msgIds: msg.msgIds });
    }).catch((err) => {
      log.error('SSE ack 失败', err);
    });
    sendResponse?.({ ok: true });
    return false;
  }

  return false;
});
} else {
  log.warn('chrome.runtime.onMessage 不可用，跳过消息监听器注册');
}

// ---- EventSource 管理 ----

async function ensureSSEConnection(channel, accountId) {
  const key = `${channel}:${accountId}`;
  if (sseConnections.has(key)) return sseConnections.get(key);

  // 读取保存的 Last-Event-ID
  let lastEventID = '';
  try {
    const stored = await chrome.storage.local.get(`last_event_${key}`);
    lastEventID = stored[`last_event_${key}`] || '';
  } catch (_) {}

  const baseUrl = await getBaseUrl();
  const token = await getAuthToken();

  const url = `${baseUrl}/api/bridge/outbox/sse?channel=${encodeURIComponent(channel)}&account_id=${encodeURIComponent(accountId)}&last_event_id=${encodeURIComponent(lastEventID)}`;
  // EventSource 不支持自定义 Header，Token 只能通过查询参数；
  // 服务端闸门读的键名是 bridge_token（middleware/bridge_ingress_guard.go），
  // 写成 token 会被当成"没带凭证"直接 401。
  const fullUrl = token ? `${url}&bridge_token=${encodeURIComponent(token)}` : url;

  log.info(`SSE 连接中: ${key}, lastEventID=${lastEventID || '(none)'}`);

  try {
    const es = new EventSource(fullUrl);
    let connected = false;

    es.addEventListener('open', () => {
      connected = true;
      log.info(`SSE 已连接: ${key}`);
    });

    es.addEventListener('message', (event) => {
      try {
        const data = JSON.parse(event.data);
        // 保存 Last-Event-ID
        if (event.lastEventId) {
          chrome.storage.local.set({ [`last_event_${key}`]: event.lastEventId }).catch(() => {});
        }
        lastEventIDs.set(key, event.lastEventId);
        // 路由到对应标签页
        dispatchToTabs(channel, accountId, data);
      } catch (err) {
        log.error('SSE 消息解析失败', err);
      }
    });

    es.addEventListener('error', () => {
      log.warn(`SSE 连接错误: ${key}, 将在 ${SSE_RECONNECT_DELAY_MS}ms 后重连`);
      es.close();
      sseConnections.delete(key);
      // 清理 lastEventID 内存缓存
      lastEventIDs.delete(key);
      // 重连
      setTimeout(() => {
        ensureSSEConnection(channel, accountId).catch((err) => {
          log.error('SSE 重连失败', err);
        });
      }, SSE_RECONNECT_DELAY_MS);
    });

    sseConnections.set(key, es);
    return es;
  } catch (err) {
    log.error('EventSource 创建失败', err);
    // 重试
    setTimeout(() => {
      ensureSSEConnection(channel, accountId).catch(() => {});
    }, SSE_RECONNECT_DELAY_MS);
    throw err;
  }
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
              const es = sseConnections.get(key);
              if (es) {
                es.close();
                sseConnections.delete(key);
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
      for (const [key, es] of sseConnections) {
        if (es.readyState === EventSource.CLOSED) {
          log.warn(`SSE 连接已关闭，尝试重连: ${key}`);
          const [channel, accountId] = key.split(':');
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
        for (const [channel, acctMap] of Object.entries(accounts)) {
          if (!acctMap || typeof acctMap !== 'object') continue;
          for (const [accountId, acctCfg] of Object.entries(acctMap)) {
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
  for (const [, es] of sseConnections) {
    try { es.close(); } catch (_) {}
  }
  sseConnections.clear();
  lastEventIDs.clear();
  tabRegistry.clear();
}