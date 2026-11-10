import { postIngest, HTTP_INGEST_DEFAULTS } from './http-ingest.js';
import { DEFAULT_USER_SERVER, BRIDGE_THREE_CHANNEL } from './constants.js';
import { contentHash, typedContentHash, isMediaMsgType } from './types.js';
import { createLogger } from './logger.js';

const log = createLogger('uplink');

// 前端完成消息 hash（稳定幂等）：同一消息产出同一 id。
// 2026-11-10 审计 F7：媒体消息（msg_type 非 text）改用 typedContentHash（并入
// msg_type+media_url，与服务端 ContentHashMsgIDTyped 同构）——旧口径对「[图片]」
// 占位符 content 只算 content 维度，兜底生成的 event_id/content_hash 会让同会话
// 两张不同图共享身份。文本消息逐字节不变。
export function computeMsgID({ channel, accountId, conversationId, content, msgType, mediaUrl }) {
  if (isMediaMsgType(msgType)) {
    return typedContentHash(channel, conversationId, msgType, mediaUrl || '', content || '');
  }
  return contentHash(channel, conversationId, content);
}

// Uplink：统一上报队列。所有渠道经 enqueue(message) 推入，按 (accountId|conversationId)
// 短窗口合并为一次 POST，显著降低冗余请求；消息 hash 在此兜底补全。
export class Uplink {
  constructor({ channel, getConfig, retryOpts } = {}) {
    this.channel = channel;
    this.getConfig = getConfig || (async () => ({}));
    this.retryOpts = retryOpts || null;
    this.buffers = new Map();
    this.mergeWindowMs = BRIDGE_THREE_CHANNEL.uplinkMergeWindowMs;
    this.maxBatch = BRIDGE_THREE_CHANNEL.uplinkMaxBatch;
    this._confirmed = new Set();
    this._confirmedLoaded = false;
    // M-1（2026-11-11）：服务端逐条明确拒绝（accepted=false 且非 duplicate）的 event_id
    // 落放弃集合——与 _confirmed 同为终态，均不再上行（审计 M-1(a)：rejected 不在 confirmed
    // 集合导致每轮 flush 无上限重传）。
    this._abandoned = new Set();
    this._abandonedLoaded = false;
    // 实例级计数（仓库暂无 metrics 基建，沿用简单计数惯例）：
    // rejectedDropped = 服务端明确拒绝后放弃的条数（逐条 rejected + 4xx 整批拒绝）；
    // requeued = 瞬态失败回插待重传条数。
    this.metrics = { rejectedDropped: 0, requeued: 0 };
  }

  // 终态集合（confirmed / abandoned）的 chrome.storage 读取：键 mtk_uplink_<name>，
  // 失败/无 storage 环境一律降级为内存态（返回 null 表示未读到）。
  async _loadSet(key) {
    try {
      if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
        const r = await chrome.storage.local.get(key);
        const arr = (r && r[key]) || [];
        if (Array.isArray(arr)) return new Set(arr);
      }
    } catch (_) {
    }
    return null;
  }

  async _loadConfirmed() {
    if (this._confirmedLoaded) return;
    this._confirmed = (await this._loadSet('mtk_uplink_confirmed')) || this._confirmed;
    this._confirmedLoaded = true;
  }

  async _loadAbandoned() {
    if (this._abandonedLoaded) return;
    this._abandoned = (await this._loadSet('mtk_uplink_abandoned')) || this._abandoned;
    this._abandonedLoaded = true;
  }

  // 写入串行化沿用 _saveChain；超过 5000 条裁剪到最近 2000（同原 confirmed 口径）。
  async _saveSet(key, set) {
    try {
      if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
        const write = () => chrome.storage.local.set({ [key]: [...set] });
        this._saveChain = (this._saveChain || Promise.resolve()).then(write).catch(() => {});
        await this._saveChain;
      }
    } catch (_) {
    }
  }

  async _saveConfirmed() {
    if (this._confirmed.size > 5000) {
      this._confirmed = new Set([...this._confirmed].slice(-2000));
    }
    await this._saveSet('mtk_uplink_confirmed', this._confirmed);
  }

  async _saveAbandoned() {
    if (this._abandoned.size > 5000) {
      this._abandoned = new Set([...this._abandoned].slice(-2000));
    }
    await this._saveSet('mtk_uplink_abandoned', this._abandoned);
  }

  // 终态判定：confirmed（服务端已收 / duplicate 已存在）或 abandoned（服务端明确拒绝）
  // 均不再上行。
  _isSettled(eventId) {
    return !!eventId && (this._confirmed.has(eventId) || this._abandoned.has(eventId));
  }

  // M-1（2026-11-11）：ingest 响应逐条结果分三类落账（审计 M-1(a)）——
  //   accepted  → confirmed（服务端已收）
  //   duplicate → confirmed（服务端已有同 event_id，等同确认终态）
  //   rejected（accepted=false 且非 duplicate，如 ingress 校验不过）→ abandoned 放弃集合，
  //   记日志 + metrics 计数，绝不回插。仅登记本次上行批次内的 event_id（byEvent 防越批串写）。
  _applyIngestResults(resp, items) {
    if (!resp || !Array.isArray(resp.ingested)) return;
    const byEvent = new Set();
    for (const it of items) {
      if (it.event_id) byEvent.add(it.event_id);
    }
    let confirmedNew = 0;
    const rejected = [];
    for (const r of resp.ingested) {
      if (!r || !r.event_id || !byEvent.has(r.event_id)) continue;
      if (r.accepted || r.duplicate) {
        this._confirmed.add(r.event_id);
        confirmedNew++;
      } else {
        this._abandoned.add(r.event_id);
        rejected.push(r);
      }
    }
    if (rejected.length) {
      this.metrics.rejectedDropped += rejected.length;
      log.warn(`uplink 服务端明确拒绝 ${rejected.length} 条，放弃重传（不回插）`, {
        reasons: rejected.slice(0, 5).map((r) => r.reason || '(无 reason)'),
        event_ids: rejected.slice(0, 5).map((r) => String(r.event_id).slice(-12)),
      });
      this._saveAbandoned();
    }
    if (confirmedNew) this._saveConfirmed();
  }

  enqueue(message) {
    if (!message) return;
    const accountId = message.account_id || '';
    const conversationId = message.conversation_id || '';
    if (!accountId || !conversationId) return;
    if (!message.event_id) {
      message.event_id = computeMsgID({
        channel: message.channel || this.channel,
        accountId,
        conversationId,
        content: message.content,
        msgType: message.msg_type,
        mediaUrl: message.media_url,
      });
    }
    if (!message.content_hash) {
      // content_hash 与 event_id 同口径（F7）：媒体消息用 typed 哈希。服务端消费点
      // （inbox_ingress.go 的 Extra["content_hash"] 等值查找）拿到的键由此与
      // 落库行的 typed msg_id 对齐，语义从「占位符哈希」修正为「消息身份哈希」。
      message.content_hash = computeMsgID({
        channel: message.channel || this.channel,
        accountId,
        conversationId,
        content: message.content,
        msgType: message.msg_type,
        mediaUrl: message.media_url,
      });
    }
    const key = `${accountId}|${conversationId}`;
    let buf = this.buffers.get(key);
    if (!buf) {
      buf = { items: [], timer: null };
      this.buffers.set(key, buf);
    }
    buf.items.push(message);
    if (buf.items.length >= this.maxBatch) {
      this._flush(key);
    } else if (!buf.timer) {
      buf.timer = setTimeout(() => this._flush(key), this.mergeWindowMs);
    }
  }

  async _flush(key) {
    const buf = this.buffers.get(key);
    if (!buf) return;
    this.buffers.delete(key); 
    if (buf.timer) {
      clearTimeout(buf.timer);
      buf.timer = null;
    }
    const items = buf.items;
    if (!items.length) return;
    await this._loadConfirmed();
    await this._loadAbandoned();
    const pending = items.filter((m) => !this._isSettled(m.event_id));
    if (!pending.length) return;
    const sample = pending[0];
    let cfg;
    try {
      cfg = (await this.getConfig()) || {};
    } catch (_) {
      cfg = {};
    }
    const accountId = sample.account_id || '';
    const conversationId = sample.conversation_id || '';
    if (!accountId || !conversationId) return;
    const ch = sample.channel || this.channel;
    const body = {
      v: 2,
      channel: ch,
      account_id: accountId,
      conversation_id: conversationId,
      account_name: '',
      agent_id: 0,
      // 协议白名单映射：makeUnifiedMessage 的 raw（DOM outerHTML 截断）**有意不上行**
      // —— 服务端 IngestMessage 无 Raw 字段（400KB 级外链协议面不收 DOM 快照），
      // 发了也会被 Go json 忽略。此处显式列出全部上行字段，防止「加了字段以为会透传」。
      messages: pending.map((m) => ({
        event_id: m.event_id || '',
        channel: m.channel || ch,
        account_id: accountId,
        conversation_id: conversationId,
        sender_id: m.sender_id || '',
        sender_name: m.sender_name || '',
        // M-2 客户头像链路（2026-11-11）：白名单新增 sender_avatar 透传（服务端
        // IngestMessage.SenderAvatar 已有同名字段）。抓不到为空串，服务端按空处理。
        sender_avatar: m.sender_avatar || '',
        sender_type: m.sender_type || 'customer',
        msg_type: m.msg_type || 'text',
        content: m.content || '',
        media_url: m.media_url || '',
        timestamp: m.timestamp || Date.now(),
        is_group: !!m.is_group,
        group_id: m.group_id || '',
        group_name: m.group_name || '',
        history: Array.isArray(m.history) ? m.history : [],
        content_hash: m.content_hash || '',
      })),
      timeout_ms: HTTP_INGEST_DEFAULTS.longPollTimeoutMs,
    };
    const label = `[上行 ingest 合并 ×${pending.length}]`;
    try {
      const resp = await postIngest(
        {
          serverUrl: cfg.serverUrl || DEFAULT_USER_SERVER.baseUrl,
          channel: ch,
          accountId,
          conversationId,
          token: cfg.token || '',
          tenantCode: cfg.tenantCode || '',
        },
        body,
        {
          label,
          timeoutMs: HTTP_INGEST_DEFAULTS.longPollTimeoutMs + 5000,
          ...(this.retryOpts || {}),
        }
      );
      this._applyIngestResults(resp, pending);
    } catch (e) {
      // M-1（2026-11-11）：服务端明确拒绝（HTTP 4xx——参数违规/租户解析失败/凭证失效等，
      // fetchWithRetry 已标 nonRetryable 判定重试无意义）不回插，直接放弃并计数，否则
      // 每轮 flush 无上限重传死循环打无效请求（审计 M-1(a)）。不入 _abandoned 持久集合：
      // 4xx 拒绝的是整次请求且原因多为配置类，消息本体可能无辜——放弃本次即可，
      // 重新捕获时允许一次新尝试。
      if (e && e.nonRetryable) {
        this.metrics.rejectedDropped += items.length;
        log.warn(`uplink flush 被服务端拒绝（4xx），${items.length} 条放弃重传`, String(e?.message || e));
        return;
      }
      // B4（批3）：原 catch(e){} 吞错且 buffer 已从 map 摘除 = 瞬态网络故障直接丢消息。
      // 修复：未确认条目回插队首（保留原顺序，封顶 maxBatch*5 防离线期无界堆积）+ Warn 日志。
      // 回插项不自动重排 timer：由后续 enqueue/onFlushed 驱动再 flush；flushAll 亦兜底。
      // 重传仅发生在瞬态故障（网络失败/5xx/429/408/超时）——终态（confirmed/abandoned）不回插。
      const existing = this.buffers.get(key) || { items: [], timer: null };
      const restored = items.filter((m) => !this._isSettled(m.event_id));
      existing.items = [...restored, ...existing.items].slice(-this.maxBatch * 5);
      this.buffers.set(key, existing);
      this.metrics.requeued += restored.length;
      log.warn(`uplink flush 失败，${restored.length} 条回插待重传`, String(e?.message || e));
    }
  }

  async flushAll() {
    const keys = [...this.buffers.keys()];
    await Promise.all(keys.map((k) => this._flush(k)));
  }
}

