import { postIngest, HTTP_INGEST_DEFAULTS } from './http-ingest.js';
import { DEFAULT_USER_SERVER, BRIDGE_THREE_CHANNEL } from './constants.js';
import { contentHash } from './types.js';
import { createLogger } from './logger.js';

const log = createLogger('uplink');

// 前端完成消息 hash（稳定幂等）：同一消息产出同一 id。
// 与服务端 ContentHashMsgID 严格一致（channel|conversationID|trim(content)，FNV-1a 32 位，带 mh: 前缀），
// 作为回环去重钩子2（GetByMsgID）的兜底依据。
export function computeMsgID({ channel, accountId, conversationId, content }) {
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
      });
    }
    if (!message.content_hash) {
      message.content_hash = contentHash(
        message.channel || this.channel,
        conversationId,
        message.content
      );
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

