
const ACCOUNT_CACHE_KEY = 'bridgeAccountFallback';

// 8 account_id 多层 fallback 派生 ——
// 全部候选为空时返回 `${channel}-unknown`（稳定占位，绝不空串——空 account_id 会被
// 服务端 401 拒绝、上行全断）。改为稳定 `${channel}-unknown` 后：上行不中断，
// 后端按 (platform + sender_name + content) 三元组命中 outbound
// （见 service/inbox_ingress_outbound.go 层0 回采），unknown 仅为身份兜底。
export function deriveAccountId(channel, candidates) {
  for (const c of candidates) {
    if (c && typeof c === 'string' && c.trim()) return c.trim();
  }
  return `${channel || 'unknown'}-unknown`;
}

export async function getCachedAccount(channel) {
  try {
    return new Promise((resolve) => {
      chrome.storage.local.get([ACCOUNT_CACHE_KEY], (res) => {
        const m = res && res[ACCOUNT_CACHE_KEY];
        if (m && typeof m === 'object' && m[channel]) return resolve(m[channel]);
        resolve(null);
      });
    });
  } catch  {
    return null;
  }
}

export function setCachedAccount(channel, accountId) {
  if (!accountId) return;
  try {
    chrome.storage.local.get([ACCOUNT_CACHE_KEY], (res) => {
      const m = (res && res[ACCOUNT_CACHE_KEY]) || {};
      m[channel] = accountId;
      chrome.storage.local.set({ [ACCOUNT_CACHE_KEY]: m });
    });
  } catch  {
  }
}

export async function resolveAccountIdWithFallback(channel, primaryCandidates) {
  // 1) 主候选列表（按渠道 hooks 给出的 URL 链接、侧边栏链接等）
  const primary = deriveAccountId(channel, primaryCandidates);
  if (primary) {
    setCachedAccount(channel, primary);
    return primary;
  }
  // 2) 缓存（同一账号即使链接暂时取不到，仍可恢复）
  const cached = await getCachedAccount(channel);
  if (cached) return cached;
  return '';
}

// 前端统一自/他占位：自/他判定已完全移交后端（服务端内容回显检测 + sender_type 强制覆盖），
// 前端不再计算 self/other。非 system/recall 消息一律填 customer 占位，后端会强制覆盖；
// 仅 system/recall 需前端识别（消息类型，否则后端误触发 AI）。
export const FRONTEND_DEFAULT_SENDER_TYPE = 'customer';
