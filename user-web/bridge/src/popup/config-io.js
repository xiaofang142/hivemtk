
import { createLogger } from '../core/logger.js';
import { configStore } from '../core/config-store.js';

const log = createLogger('config-io', 'popup');

export const CONFIG_FILE_NAME = 'hivemtk-bridge-config.json';
export const CONFIG_SCHEMA = 1;
export const KDF_ITERATIONS = 100000;
const PBKDF2_ALGO = 'PBKDF2';
const AES_GCM = 'AES-GCM';
const NONCE_BYTES = 12;
const SALT_BYTES = 16;

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

// ---- Base64 工具（浏览器 btoa/atob；Node 用 Buffer，兼容 jsdom 缺失场景）----
function bytesToBase64(bytes) {
  const arr = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  if (typeof Buffer !== 'undefined') return Buffer.from(arr).toString('base64');
  let bin = '';
  for (let i = 0; i < arr.length; i++) bin += String.fromCharCode(arr[i]);
  return btoa(bin);
}

function base64ToBytes(b64) {
  if (typeof Buffer !== 'undefined') {
    const buf = Buffer.from(b64, 'base64');
    return new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength);
  }
  const bin = atob(b64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
  return arr;
}

// 取 Web Crypto subtle（可被注入覆盖）
export function getSubtle() {
  try {
    if (globalThis.crypto && globalThis.crypto.subtle) return globalThis.crypto.subtle;
  } catch (_) {  }
  return null;
}

// 随机字节（salt / nonce）；opts.rng 可注入（测试确定性）
function randomBytes(length, rng) {
  if (typeof rng === 'function') {
    const arr = rng(length);
    if (arr && arr.length === length) return arr;
  }
  const cryptoObj = globalThis.crypto;
  if (!cryptoObj || typeof cryptoObj.getRandomValues !== 'function') {
    throw new Error('crypto.getRandomValues 不可用，无法生成随机盐/随机数');
  }
  const out = new Uint8Array(length);
  cryptoObj.getRandomValues(out);
  return out;
}

// 序列化当前配置 → 导出明文数据对象
// 导出字段：serverUrl/token/channel/accountId/conversationId/patrol/rate/circuit/version（+accounts）
export function serializeConfig(cfg) {
  const c = cfg || {};
  return {
    schema: CONFIG_SCHEMA,
    serverUrl: c.serverUrl || '',
    token: c.token || '',
    channel: c.channel || '',
    accountId: c.accountId || '',
    conversationId: c.conversationId || '',
    patrol: c.patrol && typeof c.patrol === 'object' ? { ...c.patrol } : {},
    rate: c.rate && typeof c.rate === 'object' ? { ...c.rate } : {},
    circuit: c.circuit && typeof c.circuit === 'object' ? { ...c.circuit } : {},
    version: c.version || 1,
    accounts: c.accounts && typeof c.accounts === 'object' ? { ...c.accounts } : {},
  };
}

async function deriveAesKey(subtle, passphrase, salt, iterations, usages) {
  const baseKey = await subtle.importKey('raw', textEncoder.encode(String(passphrase || '')), PBKDF2_ALGO, false, ['deriveKey']);
  return subtle.deriveKey(
    { name: PBKDF2_ALGO, salt, iterations, hash: 'SHA-256' },
    baseKey,
    { name: AES_GCM, length: 256 },
    false,
    usages
  );
}

// 加密导出：encryptJSON(plain, passphrase, opts)
// 返回结构：{ schema, encrypted:true, kdf:{algo,salt,iterations}, nonce, ciphertext }
// opts: { subtle, iterations, rng, salt?, nonce? } 可注入（测试用）
export async function encryptJSON(plain, passphrase, opts = {}) {
  const subtle = opts.subtle || getSubtle();
  if (!subtle) throw new Error('Web Crypto API 不可用（crypto.subtle 未定义）');
  const iterations = opts.iterations || KDF_ITERATIONS;
  const salt = opts.salt || randomBytes(SALT_BYTES, opts.rng);
  const nonce = opts.nonce || randomBytes(NONCE_BYTES, opts.rng);
  const key = await deriveAesKey(subtle, passphrase, salt, iterations, ['encrypt']);
  const ciphertext = await subtle.encrypt({ name: AES_GCM, iv: nonce }, key, textEncoder.encode(JSON.stringify(plain)));
  return {
    schema: CONFIG_SCHEMA,
    encrypted: true,
    kdf: { algo: PBKDF2_ALGO, salt: bytesToBase64(salt), iterations },
    nonce: bytesToBase64(nonce),
    ciphertext: bytesToBase64(ciphertext),
  };
}

// 解密导入：decryptJSON(payload, passphrase, opts) → 明文数据对象
// 明文结构 { encrypted:false, data:{...} } 直接返回 data；加密结构先解密再 JSON.parse。
// 口令错误时（GCM 校验失败）抛 Error；解密结果非 JSON 抛 Error。
export async function decryptJSON(payload, passphrase, opts = {}) {
  const subtle = opts.subtle || getSubtle();
  if (!subtle) throw new Error('Web Crypto API 不可用（crypto.subtle 未定义）');
  if (!payload || typeof payload !== 'object') throw new Error('无效的配置文件');
  if (!payload.encrypted) {
    if (payload.data && typeof payload.data === 'object') return payload.data;
    throw new Error('明文配置文件缺少 data 字段');
  }
  const { kdf, nonce, ciphertext } = payload;
  if (!kdf || !nonce || !ciphertext) throw new Error('加密配置字段缺失（kdf/nonce/ciphertext）');
  const iterations = Number.isFinite(kdf.iterations) && kdf.iterations > 0 ? kdf.iterations : KDF_ITERATIONS;
  const key = await deriveAesKey(subtle, passphrase, base64ToBytes(kdf.salt), iterations, ['decrypt']);
  let plainBuf;
  try {
    plainBuf = await subtle.decrypt({ name: AES_GCM, iv: base64ToBytes(nonce) }, key, base64ToBytes(ciphertext));
  } catch (e) {
    throw new Error('解密失败：口令错误或文件已损坏', { cause: e });
  }
  const text = textDecoder.decode(plainBuf);
  try {
    return JSON.parse(text);
  } catch (e) {
    throw new Error('解密后的内容不是合法 JSON', { cause: e });
  }
}

async function readCurrentConfig(opts) {
  if (opts && opts.store) return opts.store.getConfig();
  try {
    if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) {
      const r = await chrome.storage.local.get('bridgeConfig');
      if (r && r.bridgeConfig && typeof r.bridgeConfig === 'object') return r.bridgeConfig;
    }
  } catch (_) {  }
  return configStore.getConfig();
}

// 导出：读取当前配置并序列化
// 返回 { text, filename, payload }；UI 层负责触发下载
export async function exportConfig(passphrase, opts = {}) {
  const cfg = await readCurrentConfig(opts);
  const plain = serializeConfig(cfg);
  const payload = passphrase && String(passphrase).trim()
    ? await encryptJSON(plain, passphrase, opts)
    : { schema: CONFIG_SCHEMA, encrypted: false, data: plain };
  return {
    text: JSON.stringify(payload, null, 2),
    filename: CONFIG_FILE_NAME,
    payload,
  };
}

// 导入：解析上传文件（File 或字符串），解密/解析成功后热更新 configStore
// 返回 { ok:true, config }；任何失败抛 Error（message 为中文提示）
export async function importConfig(fileOrText, passphrase, opts = {}) {
  let text = fileOrText;
  if (fileOrText && typeof fileOrText.text === 'function') {
    text = await fileOrText.text();
  }
  let parsed;
  try {
    parsed = JSON.parse(String(text || ''));
  } catch (e) {
    throw new Error('非法 JSON 文件，无法解析', { cause: e });
  }
  if (!parsed || typeof parsed !== 'object') throw new Error('非法 JSON 文件，内容不是对象');

  let data;
  if (parsed.encrypted) {
    data = await decryptJSON(parsed, passphrase, opts);
  } else if (parsed.data && typeof parsed.data === 'object') {
    data = parsed.data;
  } else {
    throw new Error('配置文件结构无效（缺少 data 或 encrypted 标记）');
  }
  if (!data || typeof data !== 'object') throw new Error('配置数据无效');

  const patch = {};
  for (const key of ['serverUrl', 'token', 'channel', 'accountId', 'conversationId']) {
    if (typeof data[key] === 'string') patch[key] = data[key];
  }
  for (const key of ['patrol', 'rate', 'circuit', 'accounts']) {
    if (data[key] && typeof data[key] === 'object') patch[key] = data[key];
  }
  const store = opts.store || configStore;
  const cfg = await store.set(patch);
  log.info('配置导入成功，已热更新', { serverUrl: patch.serverUrl || '', fields: Object.keys(patch).length });
  return { ok: true, config: cfg };
}

// 触发浏览器下载 JSON 文本（仅 popup 环境调用）
export function downloadConfigJson(text, filename) {
  try {
    const blob = new Blob([text], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename || CONFIG_FILE_NAME;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(() => { try { URL.revokeObjectURL(url); } catch (_) {  } }, 1000);
    return { ok: true };
  } catch (e) {
    log.warn('触发下载失败', e && e.message);
    return { ok: false, error: String((e && e.message) || e) };
  }
}

if (typeof window !== 'undefined') {
  window.__configIO = {
    serializeConfig,
    encryptJSON,
    decryptJSON,
    exportConfig,
    importConfig,
    downloadConfigJson,
    getSubtle,
    CONFIG_FILE_NAME,
    CONFIG_SCHEMA,
  };
}

