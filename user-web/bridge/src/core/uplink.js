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
  }

  async _loadConfirmed() {
    if (this._confirmedLoaded) return;
    try {
      if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
        const r = await chrome.storage.local.get('mtk_uplink_confirmed');
        const arr = (r && r.mtk_uplink_confirmed) || [];
        if (Array.isArray(arr)) this._confirmed = new Set(arr);
      }
    } catch (_) {
    }
    this._confirmedLoaded = true;
  }

  async _saveConfirmed() {
    try {
      if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
        const write = () => {
          if (this._confirmed.size > 5000) {
            this._confirmed = new Set([...this._confirmed].slice(-2000));
          }
          return chrome.storage.local.set({ mtk_uplink_confirmed: [...this._confirmed] });
        };
        this._saveChain = (this._saveChain || Promise.resolve()).then(write).catch(() => {});
        await this._saveChain;
      }
    } catch (_) {
    }
  }

  _markConfirmedFromResponse(resp, items) {
    if (!resp || !Array.isArray(resp.ingested)) return;
    const byEvent = new Map();
    for (const it of items) {
      if (it.event_id) byEvent.set(it.event_id, true);
    }
    for (const r of resp.ingested) {
      if (r && (r.accepted || r.duplicate) && r.event_id && byEvent.has(r.event_id)) {
        this._confirmed.add(r.event_id);
      }
    }
    if (this._confirmed.size) this._saveConfirmed();
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
    const pending = items.filter((m) => !(m.event_id && this._confirmed.has(m.event_id)));
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
      this._markConfirmedFromResponse(resp, pending);
    } catch (e) {
      // B4（批3）：原 catch(e){} 吞错且 buffer 已从 map 摘除 = 瞬态网络故障直接丢消息。
      // 修复：未确认条目回插队首（保留原顺序，封顶 maxBatch*5 防离线期无界堆积）+ Warn 日志。
      // 回插项不自动重排 timer：由后续 enqueue/onFlushed 驱动再 flush；flushAll 亦兜底。
      const existing = this.buffers.get(key) || { items: [], timer: null };
      const restored = items.filter((m) => !(m.event_id && this._confirmed.has(m.event_id)));
      existing.items = [...restored, ...existing.items].slice(-this.maxBatch * 5);
      this.buffers.set(key, existing);
      log.warn(`uplink flush 失败，${restored.length} 条回插待重传`, String(e?.message || e));
    }
  }

  async flushAll() {
    const keys = [...this.buffers.keys()];
    await Promise.all(keys.map((k) => this._flush(k)));
  }
}

