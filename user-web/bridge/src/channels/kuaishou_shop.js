import { BaseAdapter } from '../core/channel-adapter.js';
import { CHANNELS } from '../core/types.js';
import { createLogger } from '../core/dom.js';

// CH-19 快手小店(快语) 商家客服网页桥——fail-closed 骨架（2026-11-18 服务端全链注册批）。
// 状态：入口域 P 级证据在案（快语客服工作台，登录壳已实证、DOM 未知，选择器零证据——19a 授权账号校准）。
// 边界：SEL 全空=选择器零证据，本适配器**结构性拒启**（match() 恒 false、
// getConversationId 恒 null、sendText/sendMedia 恒抛错）——不造选择器、不冒充
// 收发可用；真机校准（授权账号现场取 DOM）前保持 Experimental。校准完成时
// 按既有 pdd/京麦模式补候选选择器并放开 match()，先补 fixture 再开闸。
const log = createLogger('kuaishou_shop', CHANNELS.KUAISHOU_SHOP);

// 选择器：零证据。键保留与 pdd.js 同名结构，校准时逐键填入，防漂移。
export const SEL = {
  CHAT_LIST: '',
  CONV_ITEM: '',
  UNREAD: '',
  MSG_LIST: '',
  MSG_ITEM: '',
  TEXT: '',
  INPUT: '',
  SEND: '',
  PEER_NAME: '',
  SYSTEM: '',
  SELF_ITEM: '',
  BUYER_ITEM: '',
};

const HOST_SUFFIX = 'im.kwaixiaodian.com';

function isHost() {
  try {
    const h = (location.hostname || '').toLowerCase();
    return h === HOST_SUFFIX || h.endsWith('.' + HOST_SUFFIX);
  } catch (_) {
    return false;
  }
}

const hooks = {
  // fail-closed：未校准不启动（含授权域也不启动——启动了也采不到任何东西）。
  match() {
    return false;
  },
  matchMode() {
    return null;
  },
  selectors: SEL,
  mergedSelectors() {
    return { itemSelectors: [], listSelectors: [], textSelectors: [], inputSelectors: [], sendSelectors: [] };
  },
  getMessageRoot() {
    return null;
  },
  getMessageListRoot() {
    return null;
  },
  getMessageItems() {
    return [];
  },
  parseMessageItem() {
    return null;
  },
  getAccountId() {
    return '';
  },
  getConversationId() {
    return null;
  },
  getConversationList() {
    return [];
  },
  getPeerName() {
    return '';
  },
  getPeerAvatar() {
    return '';
  },
  async sendText() {
    throw new Error('kuaishou_shop: 选择器未校准（fail-closed）——CH-19 19a 授权核验 真机校准完成前拒发，不冒充发送成功');
  },
  async sendMedia() {
    throw new Error('kuaishou_shop: 选择器未校准（fail-closed）——CH-19 19a 授权核验 真机校准完成前拒发');
  },
};

export { isHost };

export function buildKuaishouShopAdapter() {
  return new BaseAdapter({
    name: 'kuaishou_shop',
    channel: CHANNELS.KUAISHOU_SHOP,
    SEL,
    hooks,
    rateLimiter: undefined,
  });
}
