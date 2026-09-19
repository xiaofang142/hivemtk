import { getOutbox, ackOutbox } from './http-ingest.js';
import { sanitizeForDisplay } from './sanitize.js';
import { contentHash } from './types.js';
import { BRIDGE_THREE_CHANNEL, RATE_LIMIT_DEFAULTS, BRIDGE_PROTOCOL_V2, DEFAULT_USER_SERVER, humanSendTimeoutMs } from './constants.js';
import { createLogger } from './logger.js';
import { connectSSE, getLastEventID, setLastEventID, stopSSE } from './sse-fetch-client.js';

const log = createLogger('downlink');

// 本地已发缓存：防刷新 / SW 冻结重开后重复下发同一条消息给用户。
// 按 channel 隔离，持久化到 chrome.storage.local。
class SentCache {
  constructor(channel) {
    this.channel = channel;
    this.key = `bridge_sent_${channel}`;
    this.mem = new Set();
    this.dirty = false;
    this.loaded = false;
  }

  async load() {
    if (this.loaded) return;
    try {
      const v = await chrome.storage.local.get([this.key]);
      const arr = v && v[this.key];
      if (Array.isArray(arr)) this.mem = new Set(arr);
    } catch (_) {
    }
    this.loaded = true;
  }

  has(id) {
    return this.mem.has(id);
  }

  add(id) {
    this.mem.add(id);
    this.dirty = true;
    if (this.mem.size > BRIDGE_THREE_CHANNEL.sentCacheMax) {
      const arr = [...this.mem];
      this.mem = new Set(arr.slice(arr.length - BRIDGE_THREE_CHANNEL.sentCacheMax));
    }
  }

  async flush() {
    if (!this.dirty) return;
    try {
      await chrome.storage.local.set({ [this.key]: [...this.mem] });
      this.dirty = false;
    } catch (_) {
    }
  }
}

const caches = {};
function getCache(channel) {
  if (!caches[channel]) caches[channel] = new SentCache(channel);
  return caches[channel];
}

// _pendingAckByChannel：按渠道维护"待重试 ack"的 msg_id 队列（2026-08-15 P0-9 升级为带退避的 Map）。
//
// 2026-08-14 修复（用户诉求 "不要兜底逻辑会导致垃圾数据" 的反例——这里必须"兜底"）：
//   sendOutbound 已成功 = 用户已收到。这条事实是"用户是否收到"的唯一权威。
//   ack 服务端只是后端记账的副作用——若 ack 失败（网络/服务暂时不可用），
//   必须 (a) 立刻写本地已发缓存防重发 (b) 记入此队列等下轮重发 ack。
//
// 2026-08-15 P0-9 升级（10/10 任务清单）：
//   - 由 Set 升级为 Map<msg_id, { attempts, firstSeenAt, lastTryAt, lastError }>
//   - 加入最大重试次数（MAX_ACK_RETRY_ATTEMPTS=10），超过则放弃重试但仍保留 cache
//     （用户已收到，后端状态机不一致无影响；避免永久重试导致队列膨胀）
//   - 加入指数退避（baseMs=1000 → 1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, 60s, 60s）
//   - 加入条目级 TTL（MAX_ACK_ENTRY_AGE_MS=24h），超过则清理（防永久残留）
const MAX_ACK_RETRY_ATTEMPTS = 10;
const MAX_PENDING_ACK_PER_CHANNEL = 1000;
const ACK_RETRY_BACKOFF_BASE_MS = 1000;     
const ACK_RETRY_BACKOFF_CAP_MS = 60_000;    
const MAX_ACK_ENTRY_AGE_MS = 24 * 60 * 60 * 1000; 
const _pendingAckByChannel = {};
function this_pendingAckFor(channel) {
  if (!_pendingAckByChannel[channel]) _pendingAckByChannel[channel] = new Map();
  const m = _pendingAckByChannel[channel];
  if (m.size > MAX_PENDING_ACK_PER_CHANNEL) {
    // 容量保护：Map 保持插入序（FIFO），直接遍历迭代器删除前 N 个最早条目。
    // 优化前：构造 [...entries()] 全量数组 + sort（O(n log n)）+ 截取；现 O(n) 一遍过。
    const iter = m.keys();
    const evictCount = m.size - MAX_PENDING_ACK_PER_CHANNEL;
    for (let i = 0; i < evictCount; i++) m.delete(iter.next().value);
  }
  return m;
}

// ackRetryBackoffMs 计算指定 attempt 次数（从 1 开始）的退避时长（毫秒）。
// 第 1 次：baseMs=1000；第 2 次：2000；第 3 次：4000；...；第 7 次及之后：capMs=60000。
export function ackRetryBackoffMs(attempt) {
  if (attempt < 1) return ACK_RETRY_BACKOFF_BASE_MS;
  const ms = ACK_RETRY_BACKOFF_BASE_MS * Math.pow(2, attempt - 1);
  return Math.min(ms, ACK_RETRY_BACKOFF_CAP_MS);
}

// addPendingAck 添加 msg_id 到重试队列（首次入队时设置 firstSeenAt=now, attempts=0）。
// B2（2026-09-19）：条目携带 conversationId（DB 行的原始 conversation_id，非 DM 重映射后的
// 会话键），重试 ack 时走 v2 items[] 按会话精确翻转。同 msg_id 跨会话入队极少见，
// 以最后一次入队的 conv 为准（重试幂等，最坏退化为 legacy 翻转范围）。
export function addPendingAck(channel, msgId, lastError, conversationId) {
  const m = this_pendingAckFor(channel);
  if (m.has(msgId)) {
    const entry = m.get(msgId);
    entry.lastTryAt = Date.now();
    if (lastError) entry.lastError = lastError;
    if (conversationId) entry.conversationId = conversationId;
    return entry;
  }
  const now = Date.now();
  const entry = { attempts: 0, firstSeenAt: now, lastTryAt: 0, lastError: lastError || '', conversationId: conversationId || '' };
  m.set(msgId, entry);
  if (m.size > MAX_PENDING_ACK_PER_CHANNEL) {
    const sorted = [...m.entries()].sort((a, b) => a[1].firstSeenAt - b[1].firstSeenAt);
    const evictCount = m.size - MAX_PENDING_ACK_PER_CHANNEL;
    for (let i = 0; i < evictCount; i++) m.delete(sorted[i][0]);
  }
  return entry;
}

// claimDuePendingAck 提取本轮可重试的 msg_id（已达退避时长 且 未超最大次数 且 未超 TTL）。
export function claimDuePendingAck(channel) {
  const m = this_pendingAckFor(channel);
  const now = Date.now();
  const due = [];
  for (const [msgId, entry] of m) {
    if (now - entry.firstSeenAt > MAX_ACK_ENTRY_AGE_MS) {
      m.delete(msgId);
      continue;
    }
    if (entry.attempts >= MAX_ACK_RETRY_ATTEMPTS) {
      m.delete(msgId);
      log.warn(`下行 ack 达到最大重试次数 ${MAX_ACK_RETRY_ATTEMPTS}，放弃重试（cache 保留防内容重发）`,
        { channel, msg_id: msgId, attempts: entry.attempts, last_error: entry.lastError });
      continue;
    }
    const backoff = ackRetryBackoffMs(entry.attempts + 1);
    if (entry.lastTryAt && now - entry.lastTryAt < backoff) continue;
    due.push({ msgId, conversationId: entry.conversationId || '', entry });
  }
  return due;
}

// markPendingAckTried 在重试后更新 attempts/lastTryAt。
export function markPendingAckTried(channel, msgId, success, error) {
  const m = this_pendingAckFor(channel);
  if (success) {
    m.delete(msgId);
    return;
  }
  const entry = m.get(msgId);
  if (!entry) return;
  entry.attempts += 1;
  entry.lastTryAt = Date.now();
  if (error) entry.lastError = error;
}

// getPendingAckStats 用于监控/调试（外部可读取重试队列状态）。
export function getPendingAckStats(channel) {
  const m = _pendingAckByChannel[channel];
  if (!m) return { size: 0 };
  let maxAttempts = 0;
  let oldest = 0;
  const now = Date.now();
  for (const [, entry] of m) {
    if (entry.attempts > maxAttempts) maxAttempts = entry.attempts;
    const age = now - entry.firstSeenAt;
    if (age > oldest) oldest = age;
  }
  return { size: m.size, maxAttempts, oldestAgeMs: oldest };
}

// 初始化：预加载各渠道已发缓存 + 待重试 ack 集合（在轮询开始前调用一次）
export async function initDownlink(channels) {
  for (const ch of channels) {
    await getCache(ch).load();
    this_pendingAckFor(ch); 
  }
}

// pollDownlink 通道C·下发轮询 + 通道B·状态确认。
// 拉取本渠道待发消息 → 按 conversation_id 分组并行转发到对应网页会话 → 成功后批量 ack delivered。
//
// 2026-08-07 修复（用户诉求 ①②③）：
//   ① 下发转发不应该串行——回复谁的消息就应该在谁的消息输入框。
//      旧版 for...of 串行处理：第 1 条发送成功 markSent → lastGlobalSendAt 立刻更新 → 第 2 条
//      tryAcquire 命中 minIntervalMs(1500ms) → 整批 break → 第二个会话永远不发。
//      改为按 conversation_id 分组并行：每个会话独立 tryAcquire/markSent/rate-limit，
//      会话 A 限速拦截不影响会话 B；同时同一会话内按原顺序串行（保留单会话限速语义）。
//   ② 下发转发应该即时——并行处理 + 会话内串行减少总等待时间。
//   ③ 每次下发转发消息应该清空输入框内容——见 xhs.js sendText fillContentEditable 调用前先清空。
export async function pollDownlink(channel, accountId, getConfig, options = {}) {
  const sendOutbound = options && typeof options.sendOutbound === 'function' ? options.sendOutbound : null;
  // 下发转发超时（实现 BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs）：sendOutbound 卡死时
  // 不应永久阻塞整个轮询，超时则视为失败，下个周期重试。
  const sendTimeoutMs = options && options.sendOutboundTimeoutMs
    ? options.sendOutboundTimeoutMs
    : BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs;
  const outboxBatchSize = options && options.outboxBatchSize
    ? options.outboxBatchSize
    : BRIDGE_THREE_CHANNEL.outboxBatchSize;
  const cache = getCache(channel);
  await cache.load();
  let cfg;
  try {
    cfg = (await getConfig()) || {};
  } catch (_) {
    cfg = {};
  }
  const serverUrl = cfg.serverUrl || '';
  const token = cfg.token || '';
  if (!serverUrl) return;

  // 2026-08-15 P0-9：先消化上轮残留的 _pendingAck（带退避，attempts<MAX_ACK_RETRY_ATTEMPTS）。
  // 必要性：ack 是"后端记账"，失败不能让"用户已收到"这件事回滚。
  // 节奏：claimDuePendingAck 自动按退避时长筛本轮可重试的条目；逐条重试；attempts++ 直到达上限。
  // 关键：每条 msg_id 单独走 ackOutbox，与正常下发同接口（避免实现分裂）。
  const duePending = claimDuePendingAck(channel);
  if (duePending.length) {
    let successCount = 0;
    let failCount = 0;
    for (const { msgId, conversationId } of duePending) {
      try {
        const reAck = await ackOutbox(
          { serverUrl, channel, accountId, token },
          [msgId],
          {
            label: `[下行 ack 重试] ${channel}:${msgId}`,
            // B2：条目带会话归属时走 v2，按会话精确翻转；无归属（老数据）回退 legacy。
            conversationId,
          }
        );
        // 详细 ack 响应：acked/duplicate 视为成功，not_found/not_in_scope 也视为"已处理（无需再发）"
        const handled = processAckDetailedResult(reAck, [msgId], channel, 'reAck');
        const ok = handled.acked + handled.duplicate + handled.not_found + handled.not_in_scope;
        if (ok > 0) {
          markPendingAckTried(channel, msgId, true);
          successCount++;
        } else {
          markPendingAckTried(channel, msgId, false,
            reAck && reAck.status === 'ok' ? 'retriable' : 'response_not_ok');
          failCount++;
        }
      } catch (e) {
        markPendingAckTried(channel, msgId, false, String(e && e.message || e));
        failCount++;
      }
    }
    if (successCount || failCount) {
      log.info(`pendingAck 重发 ack 完成`, {
        channel, success: successCount, fail: failCount, retried: duePending.length,
      });
    }
  }

  const res = await getOutbox(
    { serverUrl, channel, accountId, token },
    { label: `[下行 outbox] ${channel}`, batchSize: outboxBatchSize }
  );
  const messages = (res && res.messages) || [];
  // B4（批3）：裸 console.log 全文泄露私信内容（控制台可被同浏览器其他扩展读取）
  // → 降为 verbose-gated debug 且不再携带 content 字段（脱敏=不进参数，而非事后打码）。
  log.debug('pollDownlink 分组前', {
    channel, count: messages.length, ids: messages.map((m) => m.msg_id),
  });

  // —— 按 conversation_id 分组（同会话内保留原顺序以维持单会话限速语义）——
  const groups = new Map(); 
  for (const m of messages) {
    if (!m || !m.msg_id) continue;
    // SentCache key 必须用 msg_id|conversation_id 复合键：
    //   AI 回复 msg_id = contentHash(channel, content)，不含 conversation_id（patrol 回环去重需要）。
    //   跨会话同 content 的 AI 回复 msg_id 相同，若只用 msg_id 做 key，
    //   第一会话 ack 后 cache.add(msg_id) → 第二会话 cache.has(msg_id) 命中 → 跳过，永远不下发！
    //   复合键保证不同会话的同 msg_id 消息各自独立去重。
    // 主动私信场景（后端 Extra.dm_target==='member'）：以 receiver_id（成员标识）为会话定位键，
    // 而非原群会话 conversation_id——抖音私信会话 id 即成员标识，列表匹配可打开已有私信会话。
    let convId = m.conversation_id || '_unknown_';
    if (m.receiver_id && m.receiver_id !== m.conversation_id) {
      let ex = m.extra;
      if (typeof ex === 'string') { try { ex = JSON.parse(ex); } catch (_) { ex = null; } }
      if (ex && ex.dm_target === 'member') convId = m.receiver_id;
    }
    const cacheKey = `${m.msg_id}|${convId}`;
    if (cache.has(cacheKey)) continue; 
    const raw = m.content || '';
    if (!raw) continue; 
    // XSS 防护：净化内容（控制长度、去控制字符）
    const safeContent = sanitizeForDisplay ? sanitizeForDisplay(raw) : raw;
    if (!groups.has(convId)) groups.set(convId, []);
    groups.get(convId).push({ msg: m, sanitized: safeContent });
  }

  // —— 按 conversation_id 串行转发（核心修复：杜绝跨会话污染）——
  // 背景：桥接内容脚本运行在单个 SPA 页（小红书/抖音聊天页），多个会话共用同一页面 DOM。
  //   sendOutbound 内部会 openConversation(convId) 切换右侧会话再 fillAndSend。若多个会话
  //   并行 openConversation，页面导航会相互竞争，导致 A 会话的文案被填进 B 会话的输入框——
  //   即"下发给 A 的消息却发给了 B/所有人"。故必须按会话串行，保证"切会话→填发→完成"原子，
  //   单会话内也串行（同会话限速语义）。
  // 限速处理：全局 minInterval 会让"紧接其后的另一会话"首条被 rateLimited。此时就地等待后
  //   重试（有限次），而非 break 丢弃——既保留拟人限速，又确保每会话都最终下发（不丢消息）。
  const allAckIds = [];
  const MAX_RATE_RETRIES = 3; 
  for (const [convId, group] of groups) {
    const sentIds = [];
    // B2：ack 必须带 DB 行的原始 conversation_id（不是 DM 重映射后的 convId），
    // 服务端 v2 items[] 按 (msg_id, conversation_id) 精确翻转，防跨会话同 msg_id 误翻。
    const sentEntries = [];
    const recordSent = (msg) => {
      sentIds.push(msg.msg_id);
      sentEntries.push({ msg_id: msg.msg_id, conversation_id: msg.conversation_id || '' });
    };
    for (const { msg, sanitized } of group) {
      let result = null;
      try {
        if (sendOutbound) {
          result = await withTimeout(
            sendOutbound(sanitized, convId, { viaAdapter: channel }),
            humanSendTimeoutMs(sanitized, sendTimeoutMs),
            `sendOutbound(${channel}:${convId})`
          );
        } else {
          log.warn('pollDownlink: 未提供 sendOutbound，跳过下发', channel);
        }
      } catch (e) {
        result = null;
      }
      const ok = !!(result && result.ok);
      const rateLimited = !!(result && result.rateLimited);
      const notFound = !!(result && result.notFound);
      // B4：同上——结果日志只带键与状态位，不带私信内容（verbose-gated）
      log.debug('sendOutbound 结果', {
        channel, conv_id: convId, msg_id: msg.msg_id,
        ok: !!(result && result.ok), rateLimited: !!(result && result.rateLimited),
        notFound: !!(result && result.notFound),
      });
      if (ok) { recordSent(msg); continue; }
      if (notFound) {
        log.debug(`下行目标会话不存在，跳过并留 pending: ${convId}`, { msg_id: msg.msg_id });
        continue;
      }
      if (rateLimited) {
        let delivered = false;
        for (let attempt = 0; attempt < MAX_RATE_RETRIES && !delivered; attempt++) {
          const waitMs =
            options.rateRetryWaitMs != null ? options.rateRetryWaitMs : rateRetryBaseMs();
          await sleep(waitMs);
          try {
            const r2 = await withTimeout(
              sendOutbound(sanitized, convId, { viaAdapter: channel }),
              humanSendTimeoutMs(sanitized, sendTimeoutMs),
              `sendOutbound-retry(${channel}:${convId})`
            );
            if (r2 && r2.ok) { delivered = true; recordSent(msg); }
            else if (r2 && r2.notFound) break;      
            else if (r2 && !r2.rateLimited) break;   
          } catch (e) {  }
        }
        if (delivered) continue;
        log.warn(`下行会话持续限速，放弃本会话剩余并留 pending`, {
          channel, convId, pending: group.length - sentIds.length,
        });
        break;
      }
    }
    if (sentIds.length) {
      for (const id of sentIds) cache.add(`${id}|${convId}`);
      // 第 2 步：尝试 ack 服务端（副作用：让 server 知道这条已下发）
      // B2：全量条目都带原始 conversation_id 时走 v2 items[]（按会话精确翻转）；
      // 有缺失（老服务端 payload 无该字段）→ 保守回退 legacy msg_ids（维持旧行为）。
      const origConvOf = new Map(sentEntries.map((e) => [e.msg_id, e.conversation_id]));
      const allHaveConv = sentEntries.every((e) => !!e.conversation_id);
      const ackOpts = allHaveConv
        ? { label: `[下行 ack] ${channel}:${convId}`, items: sentEntries }
        : { label: `[下行 ack] ${channel}:${convId}` };
      const ackRes = await ackOutbox(
        { serverUrl, channel, accountId, token },
        sentIds,
        ackOpts
      );
      if (ackRes && ackRes.status === 'ok') {
        // 2026-08-15 P3-D + P4-3.4 修复：详细 ack 响应——精确处理每条 msg_id
        //   - status='acked' / 'duplicate' → 已处理完成，不入 _pendingAck
        //   - status='not_found' → 服务端明确"不可能再 ack 成功"，立即停止重发（不写 cache 后续删除由后端）
        //   - items 中无对应 msg_id（响应不完整）→ 入 _pendingAck 下轮重试
        //   - items[].status 缺失或未知 → 入 _pendingAck 下轮重试（保守 retriable）
        //   - 整体响应 ok 但 items 字段缺失（老版本）→ 全量视为成功，不入 pending
        const items = Array.isArray(ackRes.items) ? ackRes.items : null;
        if (items) {
          const itemByMsgID = new Map(items.map((it) => [it.msg_id, it]));
          for (const id of sentIds) {
            const it = itemByMsgID.get(id);
            if (!it) {
              addPendingAck(channel, id, 'ack_response_missing_item', origConvOf.get(id));
              log.warn(`下行 ack 详情缺失 item: 入 _pendingAck 下轮重试`, { channel, conv_id: convId, msg_id: id });
              continue;
            }
            if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.ACKED || it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.DUPLICATE) {
              // acked / duplicate：已确认送达，无需任何动作，移出重试队列
            } else if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_FOUND) {
              log.warn(`下行 ack 详情: not_found 停止重发`, { channel, conv_id: convId, msg_id: id });
            } else if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_IN_SCOPE) {
              // P0-6：存在但归属其他账号/方向 → 服务端明确"不归我管"，立即停止重发（防越权探测）
              log.warn(`下行 ack 详情: not_in_scope 停止重发（归属他账号/方向）`, { channel, conv_id: convId, msg_id: id });
            } else {
              addPendingAck(channel, id, `unknown_status_${it.status}`, origConvOf.get(id));
              log.warn(`下行 ack 详情未知 status: 入 _pendingAck`, { channel, conv_id: convId, msg_id: id, status: it.status });
            }
          }
        }
        allAckIds.push(...sentIds);
      } else {
        for (const id of sentIds) {
          addPendingAck(channel, id, 'ack_request_failed', origConvOf.get(id));
        }
        log.warn(`下行单会话 ack 失败（已发已写 cache 防重发，纳入下轮 ack 重试队列）`, {
          channel, convId, count: sentIds.length,
        });
      }
    }
  }
  await cache.flush();
  if (allAckIds.length) log.info(`下行完成: ${channel} 共 ${allAckIds.length} 条已 ack`, { convs: groups.size });
}

export async function withTimeout(promise, ms, label) {
  if (!ms || ms <= 0) return promise;
  let timer = null;
  const timeout = new Promise((_, rej) => {
    timer = setTimeout(() => rej(new Error(`${label} timeout after ${ms}ms`)), ms);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

// sleep：限速重试等待（拟人节奏，避免被识别为机器人）。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// rateRetryBaseMs：单行限速重试的等待时长（对齐三层风控的拟人节奏）。
// 取全局最小间隔 + 随机抖动，使"紧接其后的另一会话"在等待后能通过 minInterval 校验。
function rateRetryBaseMs() {
  const { minIntervalMs, jitterMinMs, jitterMaxMs } = RATE_LIMIT_DEFAULTS;
  const jitter = jitterMinMs + Math.random() * Math.max(0, jitterMaxMs - jitterMinMs);
  return minIntervalMs + jitter;
}

// processAckDetailedResult 处理服务端详细 ack 响应（P3-D 2026-08-15 + P0-6 2026-08-15）。
//
// 输入：ackRes（{status, acked, duplicate_count, not_found_count, items: [...]}}）+ ackIds（请求的 msg_ids）
// 输出：{ acked, duplicate, not_found, not_in_scope, retriable }
//   - acked        本次成功翻转行数（status='acked'）
//   - duplicate    幂等跳过行数（status='duplicate'，本地无需重发）
//   - not_found    不存在行数（status='not_found'，本地无需再尝试）
//   - not_in_scope 归属其他账号/方向行数（status='not_in_scope'，本地无需再尝试，防越权探测）
//   - retriable    需进入下轮 _pendingAck 重试的行数（status 缺失或响应未含详情）
//
// 设计原则：
//   - status='acked' / 'duplicate' 都视为"已处理"，不应再入 pendingAck
//   - status='not_found' / 'not_in_scope' 视为"不可能再 ack 成功"，也不应入 pendingAck
//   - 响应无 items 字段（老版本）按"全量成功"处理（保持兼容）
//   - 响应 ok 但部分 msg_id 没有对应 item（极少见）→ 入 pendingAck 下轮重试
export function processAckDetailedResult(ackRes, ackIds, channel = '', label = 'ack') {
  const result = { acked: 0, duplicate: 0, not_found: 0, not_in_scope: 0, retriable: 0 };
  if (!ackRes || ackRes.status !== 'ok') {
    result.retriable = Array.isArray(ackIds) ? ackIds.length : 0;
    return result;
  }
  const items = Array.isArray(ackRes.items) ? ackRes.items : null;
  if (!items) {
    result.acked = Array.isArray(ackIds) ? ackIds.length : 0;
    return result;
  }
  // 新版本响应：per-msg-id 分类
  const itemByMsgID = new Map(items.map((it) => [it.msg_id, it]));
  for (const id of ackIds || []) {
    if (!id) continue;
    const it = itemByMsgID.get(id);
    if (!it || !it.status) {
      result.retriable++;
      continue;
    }
    if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.ACKED) result.acked++;
    else if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.DUPLICATE) result.duplicate++;
    else if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_FOUND) result.not_found++;
    else if (it.status === BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_IN_SCOPE) result.not_in_scope++;
    else result.retriable++; 
  }
  return result;
}

// =============================================================
// SSE 模式下行支持（Service Worker 推送 → Content Script 接收）
// =============================================================
// 设计：
//   - Content Script 通过 registerWithServiceWorker() 告知 SW 自己的存在
//   - SW 建立 EventSource 连接，将下行消息通过 chrome.tabs.sendMessage 推送到 Content Script
//   - Content Script 通过 listenSSEOutbound() 监听 SSE_OUTBOUND 消息
//   - 每条消息走 SentCache 防重 → 会话内串行 → sendOutbound → ack 的完整流程
//   - 保留独立的会话队列（ConversationQueue），确保同会话消息顺序执行

// ---- 会话队列：按 conversation_id 串行执行 ----
// 同一会话的消息必须串行（避免 A 会话的消息被填进 B 会话的输入框）。
// 不同会话的消息可以并行。
class ConversationQueue {
  constructor() {
    this._queues = new Map(); // convId -> Array<{fn, resolve, reject}>
    this._running = new Set(); // convIds currently being processed
  }

  // 入队：返回 Promise，在 fn 执行完成后 resolve
  enqueue(convId, fn) {
    return new Promise((resolve, reject) => {
      if (!this._queues.has(convId)) {
        this._queues.set(convId, []);
      }
      this._queues.get(convId).push({ fn, resolve, reject });
      // 若该会话未在执行中，启动处理
      if (!this._running.has(convId)) {
        this._process(convId);
      }
    });
  }

  async _process(convId) {
    if (this._running.has(convId)) return;
    this._running.add(convId);
    try {
      while (this._queues.has(convId) && this._queues.get(convId).length > 0) {
        const item = this._queues.get(convId).shift();
        try {
          const result = await item.fn();
          item.resolve(result);
        } catch (err) {
          item.reject(err);
        }
      }
      // 队列清空，移除会话
      this._queues.delete(convId);
    } finally {
      this._running.delete(convId);
    }
  }
}

const sseQueues = new Map(); // channel -> ConversationQueue
function getSSEQueue(channel) {
  if (!sseQueues.has(channel)) {
    sseQueues.set(channel, new ConversationQueue());
  }
  return sseQueues.get(channel);
}

// resolveSSEOutboundKeys 从 SSE 事件 Data 解析去重/ack 键（B1，2026-09-19）。
//
// 背景：SSE 总线路径的 Data 曾缺 msg_id → `data.msg_id || data.id` 得 undefined →
// 复合缓存键 "undefined|conv" 第二条同会话消息起全被误判重复 → 静默丢消息。
// 服务端已统一 Data（bridge.BuildOutboundSSEEvent 双路径含 msg_id），此处为二道防线：
//   - msg_id 缺失 → 用与服务端 ContentHashMsgID 同源算法回退（fail-loud error 日志）；
//   - 绝不回退 data.id（那是 hub_id 数字串，拿去 ack 必然 not_found 且污染缓存键）。
// 导出供单测直接断言键生成契约。
export function resolveSSEOutboundKeys(data, channel) {
  const convId = (data && data.conversation_id) || '_unknown_';
  let msgId = (data && data.msg_id) || '';
  if (!msgId) {
    msgId = contentHash(channel, convId, (data && data.content) || '');
    log.error('SSE 事件缺 msg_id，已用 contentHash 同源回退（服务端 Data 契约异常，须排查）', {
      channel, conv_id: convId, fallback_msg_id: msgId,
    });
  }
  return { msgId, convId };
}

// ---- 注册/注销到 Service Worker ----

export async function registerWithServiceWorker(channel, accountId) {
  try {
    await chrome.runtime.sendMessage({
      type: 'BRIDGE_REGISTER_TAB',
      channel,
      accountId,
    });
    log.info(`已注册到 Service Worker: ${channel}:${accountId}`);
  } catch (err) {
    log.warn('注册到 Service Worker 失败（可能 SW 未就绪）', err && err.message);
    throw err;
  }
}

export async function unregisterFromServiceWorker(channel, accountId) {
  try {
    await chrome.runtime.sendMessage({
      type: 'BRIDGE_UNREGISTER_TAB',
      channel,
      accountId,
    });
    log.info(`已从 Service Worker 注销: ${channel}:${accountId}`);
  } catch (_) {}
}

// ---- 监听 Service Worker 发来的 SSE 消息 ----

let _sseListeners = new Map(); // channel -> Set of listeners

export function listenSSEOutbound(channel, onMessage, onError) {
  if (!_sseListeners.has(channel)) {
    _sseListeners.set(channel, new Set());
  }
  const handler = (msg) => {
    if (!msg || msg.type !== 'SSE_OUTBOUND') return;
    if (msg.channel !== channel) return;
    try {
      onMessage(msg.data);
    } catch (err) {
      log.error('SSE 消息处理失败', err);
      if (onError) onError(err);
    }
  };
  _sseListeners.get(channel).add(handler);

  // 全局监听器（只注册一次）
  if (!_globalSSEListenerRegistered) {
    chrome.runtime.onMessage.addListener(_globalSSEDispatcher);
    _globalSSEListenerRegistered = true;
  }

  // 返回取消订阅函数
  return () => {
    const set = _sseListeners.get(channel);
    if (set) set.delete(handler);
  };
}

let _globalSSEListenerRegistered = false;
function _globalSSEDispatcher(msg) {
  if (!msg || msg.type !== 'SSE_OUTBOUND') return false;
  const channel = msg.channel;
  const set = _sseListeners.get(channel);
  if (set) {
    for (const handler of set) {
      try { handler(msg); } catch (_) {}
    }
  }
  return false;
}

// ---- SSE 模式下发主入口 ----

/**
 * startSSEDelivery 启动 SSE 模式下行。
 *
 * 修复（2026-08-18）：
 *   - 原实现通过 Service Worker 的 EventSource，在 Chrome MV3 中受限
 *   - 新实现使用 fetch + ReadableStream 在 Content Script 中直接建立连接
 *   - 优势：更稳定、支持自定义 Header、更好的错误处理
 *
 * @param {string} channel 渠道编码 (douyin/xiaohongshu/tiktok/xianyu/kuaishou)
 * @param {string} accountId 账号 ID
 * @param {object} handlers 回调函数
 * @param {function} handlers.sendOutbound 发送函数 (text, convId, opts) => Promise<{ok, rateLimited, notFound}>
 * @param {function} handlers.onMessage 消息到达回调 (msg) => void
 * @param {function} [handlers.onError] 错误回调 (err) => void
 * @param {function} [handlers.onDuplicate] 重复消息回调 (msgId) => void
 * @returns {Promise<function>} 停止函数
 */
export async function startSSEDelivery(channel, accountId, handlers) {
  const cache = getCache(channel);
  await cache.load();

  // 1. 获取配置（server URL + token）
  let cfg = {};
  try {
    if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
      const r = await chrome.storage.local.get('bridgeConfig');
      cfg = (r && r.bridgeConfig) || {};
    }
  } catch (_) {}

  const serverUrl = cfg.serverUrl || DEFAULT_USER_SERVER.baseUrl;
  const token = cfg.token || '';
  const lastEventId = getLastEventID(channel, accountId);

  // 2. 直接在 Content Script 中建立 SSE 连接（fetch + ReadableStream）
  log.info(`SSE 直连启动: ${channel}:${accountId}`, { serverUrl, hasToken: !!token, lastEventId });

  let cleanupSSE;
  let stopped = false;
  let reconnectTimer = null;
  let wakeWait = null; // B5：stop 时立即唤醒退避等待，不等满 delay
  let reconnectAttempts = 0;
  const MAX_RECONNECT_ATTEMPTS = 10;
  const RECONNECT_BASE_DELAY_MS = 1000;
  const RECONNECT_MAX_DELAY_MS = 30000;
  // B3（2026-09-19，WHATWG SSE 重连语义 + AWS full-jitter 实践）：
  //   原实现 10 次后永久 break——SSE 是下行主通道，永久放弃 = 渠道静默失联直到页面刷新。
  //   改为超限后降级为慢重连（固定间隔 + 抖动），连接恢复时 Last-Event-ID 补拉机制兜住断流缺口。
  const DEGRADED_RETRY_MS = 5 * 60_000;
  // 服务端 retry: 字段建议值（sse-fetch-client onRetry 回调写入；0=未提供，用本地退避）
  let serverRetryDelayMs = 0;

  // 带重连的 SSE 连接启动
  async function startSSEWithReconnect() {
    while (!stopped) {
      try {
        cleanupSSE = await connectSSE(channel, accountId, {
          serverUrl,
          token,
          lastEventId: getLastEventID(channel, accountId), // 每次重连获取最新的 lastEventId
          onMessage: async (data) => {
            if (stopped) return;

            // B1：键解析收敛到 resolveSSEOutboundKeys（msg_id 缺失回退 contentHash，绝不回退 data.id）
            const { msgId, convId } = resolveSSEOutboundKeys(data, channel);

            // SentCache 防重（复合键：msg_id|conversation_id）
            const cacheKey = `${msgId}|${convId}`;
            if (cache.has(cacheKey)) {
              log.info(`SSE 消息已在缓存中，跳过: ${cacheKey}`);
              handlers.onDuplicate?.(msgId);
              return;
            }

            // 按 conversation_id 入队串行执行
            const queue = getSSEQueue(channel);
            try {
              await queue.enqueue(convId, async () => {
                const raw = data.content || '';
                if (!raw) return;

                // XSS 防护：净化内容
                const safeContent = sanitizeForDisplay ? sanitizeForDisplay(raw) : raw;

                // 调用 sendOutbound 发送到网页（B4：与轮询路径对称的超时保护，
                // 预算随文案长度伸缩，防拟人键入长文被固定超时误杀）
                if (handlers.sendOutbound) {
                  const result = await withTimeout(
                    handlers.sendOutbound(safeContent, convId, { viaAdapter: channel }),
                    humanSendTimeoutMs(safeContent),
                    `sendOutbound-SSE(${channel}:${convId})`
                  );
                  const ok = !!(result && result.ok);
                  const rateLimited = !!(result && result.rateLimited);
                  const notFound = !!(result && result.notFound);

                  if (ok) {
                    // 写入缓存防重
                    cache.add(cacheKey);
                    await cache.flush();

                    // 回调通知
                    handlers.onMessage?.({
                      msgId,
                      content: safeContent,
                      conversationId: convId,
                      msgType: data.msg_type || 'text',
                      isAIReply: data.is_ai_reply || data.event === 'ai_reply',
                    });

                    // ack 服务端（B2：convId 有效时 ackOutbox 内部自动走 v2 items[]）
                    // ackOutbox 不抛错只回 {status:'error'}——两条失败路径都要入重试队列。
                    try {
                      const ackRes = await ackOutbox(
                        { serverUrl, channel, accountId, token },
                        [msgId],
                        { label: `[SSE ack] ${channel}:${convId}`, conversationId: convId }
                      );
                      if (!ackRes || ackRes.status !== 'ok') {
                        addPendingAck(channel, msgId, 'sse_ack_not_ok', data.conversation_id || '');
                      }
                    } catch (err) {
                      log.warn('SSE ack 失败，加入重试队列', err && err.message);
                      addPendingAck(channel, msgId, 'sse_ack_failed', data.conversation_id || '');
                    }
                  } else if (rateLimited) {
                    log.warn(`SSE 下行被限速: ${channel}:${convId}`, { msgId });
                  } else if (notFound) {
                    log.warn(`SSE 下行目标会话不存在: ${channel}:${convId}`, { msgId });
                  } else {
                    log.warn(`SSE 下行发送失败: ${channel}:${convId}`, { result });
                  }
                } else {
                  log.warn(`SSE startSSEDelivery: 未提供 sendOutbound`, channel);
                }
              });
            } catch (err) {
              log.error('SSE 消息处理异常', err);
              handlers.onError?.(err);
            }
          },
          onError: (err) => {
            if (!stopped) {
              log.error(`SSE 连接错误: ${channel}:${accountId}`, err);
              handlers.onError?.(err);
            }
          },
          onLastEventID: (id) => {
            setLastEventID(channel, accountId, id);
          },
          onRetry: (ms) => {
            serverRetryDelayMs = ms;
          },
        });

        // B5：stop() 经 stopSSE 中止活动流后 await 才 resolve——先判停，不误报"已建立"、不清零计数
        if (stopped) break;

        // 重置重连计数
        reconnectAttempts = 0;
        log.info(`SSE 直连已建立: ${channel}:${accountId}`);

        // 等待连接结束（正常或异常）
        // connectSSE 在流结束时 resolve，在错误时 reject
        // 这里不需要额外等待，因为如果断开了，会进入 catch 块
      } catch (err) {
        if (stopped) break;

        reconnectAttempts++;
        let delay;
        if (reconnectAttempts > MAX_RECONNECT_ATTEMPTS) {
          if (reconnectAttempts === MAX_RECONNECT_ATTEMPTS + 1) {
            log.warn(`SSE 重连超过 ${MAX_RECONNECT_ATTEMPTS} 次，降级为 ${DEGRADED_RETRY_MS / 1000}s 慢重连（不再永久放弃）: ${channel}:${accountId}`);
          }
          // 服务端 retry: 心跳值（15s）适用于普通断流重连；连续失败已达上限说明
          // 连接可能持续劣化，降级间隔只接受更长的 hint，不接受更短。
          const base = Math.max(DEGRADED_RETRY_MS, serverRetryDelayMs);
          delay = base + Math.floor(Math.random() * base * 0.2); // ±0~20% 抖动防惊群
        } else {
          // 快速恢复期（前 10 次）：坚持本地指数退避，不吃服务端 retry hint——
          // hint=心跳间隔（15s）会把首次瞬时断流的恢复延迟从 1s 拉到 15s。
          const exp = Math.min(
            RECONNECT_BASE_DELAY_MS * Math.pow(2, reconnectAttempts - 1),
            RECONNECT_MAX_DELAY_MS
          );
          delay = Math.round(exp * (0.5 + Math.random() * 0.5)); // full-jitter 变体
        }
        log.info(`SSE 断开，${delay}ms 后第 ${reconnectAttempts} 次重连: ${channel}:${accountId}`);

        // 通知上层连接已断开（前 10 次带进度提示；降级后不再刷屏，仅首次慢重连提示一次）
        if (reconnectAttempts <= MAX_RECONNECT_ATTEMPTS + 1) {
          handlers.onError?.(new Error(`SSE 断开，准备重连 (${reconnectAttempts})`));
        }

        // 等待重连延迟（B5：可被 stop 立即唤醒——清 timer + wakeWait，不等满退避）
        await new Promise((resolve) => {
          wakeWait = resolve;
          reconnectTimer = setTimeout(resolve, delay);
        });
        wakeWait = null;
        reconnectTimer = null;

        if (stopped) break;
      }
    }

    log.info(`SSE 重连循环结束: ${channel}:${accountId}`);
  }

  // 启动重连 SSE 连接（异步，不阻塞）
  startSSEWithReconnect().catch((err) => {
    log.error(`SSE 重连循环异常退出: ${channel}:${accountId}`, err);
  });

  log.info(`SSE 下行已启动（直连模式）: ${channel}:${accountId}`);

  // 返回停止函数
  return async () => {
    stopped = true;
    // B5（批3）：原实现只 await cleanupSSE——但该停止函数在流结束后才 resolve，
    // 活动长连接期间 cleanupSSE 为 undefined，stop 实际无法断开 SSE（fetch 悬挂 + 循环续命）。
    // 修复：key 级 stopSSE 立即 abort + 清退避 timer 并唤醒等待，重连循环即刻收束。
    if (reconnectTimer) clearTimeout(reconnectTimer);
    if (wakeWait) wakeWait();
    stopSSE(channel, accountId);
    if (cleanupSSE) {
      try { await cleanupSSE(); } catch (_) {}
    }
    log.info(`SSE 下行已停止: ${channel}:${accountId}`);
  };
}

