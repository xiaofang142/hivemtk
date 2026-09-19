// sse-fetch-client.js
// 基于 fetch + ReadableStream 的 SSE 客户端
//
// 为什么不用 EventSource？
//   Chrome MV3 Service Worker 中 EventSource 存在限制：
//   1. Service Worker 空闲时会被 Chrome 终止，SSE 连接断开
//   2. EventSource 不支持自定义 Header，Token 只能通过 URL 传递
//   3. 部分 Chrome 版本在 MV3 Service Worker 中不支持 EventSource
//
// 使用 fetch + ReadableStream 的优势：
//   1. 可以在 Content Script 中直接建立连接
//   2. 支持自定义 Header（Authorization）
//   3. 更好的错误处理和重连控制
//   4. Content Script 生命周期与页面绑定，更稳定

import { createLogger } from './logger.js';
import { DEFAULT_USER_SERVER } from './constants.js';

const log = createLogger('sse-fetch', 'bridge');

// 全局状态
const connections = new Map(); // key = "channel:account_id" -> { abortController, reader }
const lastEventIDs = new Map(); // key = "channel:account_id" -> last event ID
let listeners = new Map(); // key = "channel" -> Set of callbacks

// ---- 解析 SSE 流 ----

/**
 * 解析 SSE 数据流
 * @param {ReadableStreamDefaultReader} reader 
 * @param {object} handlers 
 */
async function parseSSEStream(reader, handlers) {
  let buffer = '';
  let currentEvent = { data: '', event: 'message', id: '' };

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        log.info('SSE 流结束');
        break;
      }

      // 解码
      const chunk = new TextDecoder().decode(value);
      buffer += chunk;

      // 按行解析
      const lines = buffer.split('\n');
      buffer = lines.pop(); // 保留不完整的行

      for (const line of lines) {
        const trimmed = line.trim();

        // 空行 = 事件结束
        if (trimmed === '') {
          if (currentEvent.data) {
            // 触发事件
            try {
              handlers.onEvent?.(currentEvent);
            } catch (err) {
              log.error('SSE 事件处理失败', err);
            }
            // 重置
            currentEvent = { data: '', event: 'message', id: '' };
          }
          continue;
        }

        // 注释行（:开头）
        if (trimmed.startsWith(':')) {
          // :keepalive 或 : 其他注释，忽略
          continue;
        }

        // 字段解析
        if (trimmed.startsWith('data:')) {
          const value = trimmed.slice(5).trim();
          if (currentEvent.data) {
            currentEvent.data += '\n' + value;
          } else {
            currentEvent.data = value;
          }
        } else if (trimmed.startsWith('event:')) {
          currentEvent.event = trimmed.slice(6).trim();
        } else if (trimmed.startsWith('id:')) {
          currentEvent.id = trimmed.slice(3).trim();
          // 保存 Last-Event-ID
          if (currentEvent.id) {
            handlers.onLastEventID?.(currentEvent.id);
          }
        } else if (trimmed.startsWith('retry:')) {
          const retryMs = parseInt(trimmed.slice(6).trim(), 10);
          if (!isNaN(retryMs)) {
            handlers.onRetry?.(retryMs);
          }
        }
      }
    }
  } catch (err) {
    if (err.name === 'AbortError') {
      log.info('SSE 连接被主动关闭');
    } else {
      throw err;
    }
  }
}

// ---- SSE 连接管理 ----

/**
 * 建立 SSE 连接
 * @param {string} channel 
 * @param {string} accountId 
 * @param {object} opts 
 * @param {string} opts.serverUrl 
 * @param {string} opts.token 
 * @param {string} opts.lastEventId 
 * @param {function} opts.onMessage 
 * @param {function} opts.onError 
 * @returns {Promise<function>} 停止函数
 */
export async function connectSSE(channel, accountId, opts = {}) {
  const key = `${channel}:${accountId}`;

  // 如果已连接，先关闭
  if (connections.has(key)) {
    try {
      connections.get(key).abortController.abort();
    } catch (_) {}
    connections.delete(key);
  }

  const {
    serverUrl = DEFAULT_USER_SERVER.baseUrl,
    token = '',
    lastEventId = '',
    onMessage,
    onError,
    onLastEventID,
    onRetry,
  } = opts;

  // 构建 URL
  const url = new URL(`${serverUrl}/api/bridge/outbox/sse`);
  url.searchParams.set('channel', channel);
  url.searchParams.set('account_id', accountId);
  if (lastEventId) {
    url.searchParams.set('last_event_id', lastEventId);
  }

  // 构建 Headers
  // 注意：不要使用 Cache-Control 头，它会触发 CORS 预检请求且服务器未放行
  // cache: 'no-store' 通过 fetch 选项控制，不需要自定义头
  const headers = {
    'Accept': 'text/event-stream',
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  if (lastEventId) {
    headers['Last-Event-ID'] = lastEventId;
  }

  log.info(`SSE 连接中: ${key}`, { url: url.toString().replace(serverUrl, ''), hasToken: !!token, lastEventId });

  const controller = new AbortController();

  try {
    const response = await fetch(url.toString(), {
      method: 'GET',
      headers,
      signal: controller.signal,
      cache: 'no-store',
    });

    if (!response.ok) {
      throw new Error(`SSE 连接失败: HTTP ${response.status}`);
    }

    if (!response.body) {
      throw new Error('SSE 响应没有 body');
    }

    log.info(`SSE 已连接: ${key}`);

    // 保存连接信息
    connections.set(key, { abortController: controller, response });

    // 解析流
    const reader = response.body.getReader();
    await parseSSEStream(reader, {
      onEvent: (event) => {
        if (event.id) {
          lastEventIDs.set(key, event.id);
          onLastEventID?.(event.id);
        }
        if (event.event === 'new_outbound' || event.event === 'message') {
          try {
            const data = JSON.parse(event.data);
            onMessage?.(data);
          } catch (err) {
            // 可能是心跳或系统消息
            if (event.data && event.data !== '') {
              log.debug(`SSE 非 JSON 事件: ${event.event}`, event.data?.substring?.(0, 100));
            }
          }
        }
      },
      onLastEventID: (id) => {
        lastEventIDs.set(key, id);
        onLastEventID?.(id);
      },
      onRetry: (ms) => {
        log.info(`SSE 服务端建议重连间隔: ${ms}ms`);
        // B3：透传给调用方（WHATWG retry: 仅纯数字生效，parseSSEStream 已做 isNaN 过滤）
        onRetry?.(ms);
      },
    });

    log.info(`SSE 连接结束: ${key}`);
  } catch (err) {
    if (err.name === 'AbortError') {
      log.info(`SSE 连接被关闭: ${key}`);
    } else {
      log.error(`SSE 连接错误: ${key}`, err);
      onError?.(err);
      throw err; // 让调用者决定是否重连
    }
  } finally {
    connections.delete(key);
  }

  // 返回停止函数
  return async () => {
    if (connections.has(key)) {
      connections.get(key).abortController.abort();
      connections.delete(key);
      log.info(`SSE 已停止: ${key}`);
    }
  };
}

/**
 * 获取保存的 Last-Event-ID
 */
export function getLastEventID(channel, accountId) {
  const key = `${channel}:${accountId}`;
  return lastEventIDs.get(key) || '';
}

/**
 * 设置 Last-Event-ID
 */
export function setLastEventID(channel, accountId, id) {
  const key = `${channel}:${accountId}`;
  if (id) {
    lastEventIDs.set(key, id);
  } else {
    lastEventIDs.delete(key);
  }
}

/**
 * 检查连接状态
 */
export function isConnected(channel, accountId) {
  const key = `${channel}:${accountId}`;
  return connections.has(key);
}

/**
 * B5（批3）：按 key 中止活动连接。connectSSE 的停止函数在流结束后才 resolve，
 * 无法中断进行中的长连接；调用方需要"立即断开"（如 stop/页面卸载）时走本函数，
 * 由 fetch AbortController 使 parseSSEStream 以 AbortError 收尾。
 */
export function stopSSE(channel, accountId) {
  const key = `${channel}:${accountId}`;
  if (!connections.has(key)) return false;
  try {
    connections.get(key).abortController.abort();
  } catch (_) {}
  connections.delete(key);
  return true;
}

/**
 * 关闭所有连接
 */
export function closeAll() {
  for (const [key, conn] of connections) {
    try {
      conn.abortController.abort();
    } catch (_) {}
  }
  connections.clear();
  log.info('所有 SSE 连接已关闭');
}

// 调试用
export function _getState() {
  return {
    connections: connections.size,
    keys: [...connections.keys()],
    lastEventIDs: Object.fromEntries(lastEventIDs),
  };
}
