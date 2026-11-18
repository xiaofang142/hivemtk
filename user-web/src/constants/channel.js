export const CHANNEL_GROUP = Object.freeze({
  IM: 'im',
  SOCIAL: 'social',
  NOTIFY: 'notify',
  COLLAB: 'collab',
  CARD: 'card',
  WEB: 'web'
});

export const CHANNEL_OPTIONS = Object.freeze([
  { value: 'wecom',         label: '企业微信', tagType: 'success', group: CHANNEL_GROUP.IM,     icon: 'OfficeBuilding', description: '企业微信（外部联系人）', alias: ['企微'] },
  { value: 'weixin',        label: '微信公众号', tagType: 'success', group: CHANNEL_GROUP.IM,   icon: 'ChatDotRound',    description: '微信公众号（客服消息）', alias: ['微信', 'wechat'] },
  { value: 'personal_wx',   label: '个人微信',   tagType: 'success', group: CHANNEL_GROUP.SOCIAL, icon: 'ChatDotRound',  description: '个人微信（桌面伴生；服务端协议就绪，伴生未交付）' },
  { value: 'whatsapp',      label: 'WhatsApp',  tagType: 'success', group: CHANNEL_GROUP.IM,     icon: 'ChatLineRound',   description: 'WhatsApp Cloud API（Meta 商业）', newBadge: true },
  { value: 'telegram',      label: 'Telegram',  tagType: 'primary', group: CHANNEL_GROUP.IM,     icon: 'Promotion',       description: 'Telegram Bot API（境外 IM）', newBadge: true },
  { value: 'feishu',        label: '飞书',      tagType: 'primary', group: CHANNEL_GROUP.COLLAB, icon: 'ChatLineSquare',  description: '飞书 Open API（协作）', newBadge: true },
  { value: 'dingtalk',      label: '钉钉',      tagType: 'primary', group: CHANNEL_GROUP.COLLAB, icon: 'Connection',      description: '钉钉机器人' },
  {
    value: 'douyin',
    label: '抖音',
    tagType: 'info',
    group: CHANNEL_GROUP.SOCIAL,
    icon: 'Share',
    description: '抖音私信'
  },
  { value: 'kuaishou',      label: '快手',      tagType: 'info',        group: CHANNEL_GROUP.SOCIAL, icon: 'Share',           description: '快手私信（实验，选择器未经真机校准）' },
  { value: 'xiaohongshu',   label: '小红书',    tagType: 'danger',  group: CHANNEL_GROUP.SOCIAL, icon: 'Postcard',        description: '小红书私信' },
  { value: 'xianyu',        label: '闲鱼',      tagType: 'warning', group: CHANNEL_GROUP.SOCIAL, icon: 'Goods',           description: '闲鱼私信' },
  { value: 'tiktok',        label: 'TikTok',    tagType: 'info',        group: CHANNEL_GROUP.SOCIAL, icon: 'VideoCamera',     description: 'TikTok 私信' },
  { value: 'jd',            label: '京东',      tagType: 'danger',      group: CHANNEL_GROUP.SOCIAL, icon: 'ShoppingCart',    description: '京东（京麦/咚咚私信，实验）' },
  { value: 'bilibili',      label: 'B站',       tagType: 'info',        group: CHANNEL_GROUP.SOCIAL, icon: 'VideoCamera',     description: 'B站私信（实验）' },
  { value: 'weibo',         label: '微博',      tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'ChatDotRound',    description: '微博聊天（实验）' },
  { value: 'pdd',           label: '拼多多',    tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'ShoppingCart',    description: '拼多多商家聊天（实验，选择器未经真机校准）' },
  // 2026-11-18 电商店铺五渠道骨架（CH-15/18/19/28/35）：fail-closed，选择器未经真机校准。
  { value: 'taobao',        label: '淘宝/天猫', tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'ShoppingCart',    description: '千牛网页接待中心（实验骨架，选择器未经真机校准）' },
  { value: 'douyin_shop',   label: '抖店飞鸽',  tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'Goods',           description: '抖店飞鸽客服（实验骨架，选择器未经真机校准）' },
  { value: 'kuaishou_shop', label: '快手小店',  tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'Goods',           description: '快语客服工作台（实验骨架，选择器未经真机校准）' },
  { value: 'shopee',        label: 'Shopee',    tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'ShoppingCart',    description: 'Shopee Seller Centre Webchat（实验骨架，选择器未经真机校准）' },
  { value: 'walmart_us',    label: 'Walmart',   tagType: 'warning',     group: CHANNEL_GROUP.SOCIAL, icon: 'ShoppingCart',    description: 'Walmart Seller Center Inbox（实验骨架，选择器未经真机校准）' },
  { value: 'sms',           label: '短信',      tagType: 'info',    group: CHANNEL_GROUP.NOTIFY, icon: 'Cellphone',       description: '短信触达（模板/直发）' },
  { value: 'email',         label: '邮件',      tagType: 'info',    group: CHANNEL_GROUP.NOTIFY, icon: 'Message',         description: '邮件触达（支持附件）' },
  { value: 'card',          label: '卡片',      tagType: 'info',    group: CHANNEL_GROUP.CARD,   icon: 'Postcard',        description: '卡片消息（子渠道）' },
  { value: 'web',           label: 'Web Widget', tagType: 'info',   group: CHANNEL_GROUP.WEB,    icon: 'Monitor',         description: '客服 Web Widget 渠道' },
  { value: 'web_embed',     label: '网页',      tagType: 'info',   group: CHANNEL_GROUP.WEB,    icon: 'Monitor',         description: 'Web Widget 嵌入访客端（第三方网站访客）' }
]);

export const PLATFORM_GROUP_MEMBERS = Object.freeze({
  douyin: ['douyin', 'douyin_web'],
  xiaohongshu: ['xiaohongshu', 'xhs_web'],
  tiktok: ['tiktok', 'tiktok_web'],
  kuaishou: ['kuaishou', 'kuaishou_web'],
  xianyu: ['xianyu', 'xianyu_web'],
  jd: ['jd', 'jd_web'],
  bilibili: ['bilibili', 'bilibili_web', 'bili'],
  weibo: ['weibo', 'weibo_web', 'wb'],
  // CH-16 首切片（2026-11-09）：拼多多商家聊天，canonical=pdd，别名与后端
  // bridge.NormalizeBridgeChannel（pinduoduo/pdd_web→pdd）一致。
  pdd: ['pdd', 'pdd_web', 'pinduoduo'],
  // 2026-11-18 电商店铺五渠道，别名与后端 bridge.NormalizeBridgeChannel 一致。
  taobao: ['taobao', 'taobao_web', 'tmall', 'qianniu'],
  douyin_shop: ['douyin_shop', 'douyin_shop_web', 'jinritemai', 'feige'],
  kuaishou_shop: ['kuaishou_shop', 'kuaishou_shop_web', 'kwaixiaodian', 'kuaishouxiao'],
  shopee: ['shopee', 'shopee_web', 'shopee_seller'],
  walmart_us: ['walmart_us', 'walmart', 'walmart_web'],
  personal_wx: ['personal_wx', 'wx_personal', 'wechat_personal']
});

export const PLATFORM_GROUP_MEMBERS_REVERSE = Object.freeze(
  Object.fromEntries(
    Object.entries(PLATFORM_GROUP_MEMBERS).flatMap(([g, list]) => list.map((v) => [v, g]))
  )
);

export const CHANNEL_LABEL_ALIAS = Object.freeze(
  CHANNEL_OPTIONS.reduce((acc, o) => {
    if (o.alias && o.alias.length) {
      o.alias.forEach((a) => { if (!acc[a]) acc[a] = o.label })
    }
    return acc
  }, {})
);

export const CHANNEL_LABEL_MAP = Object.freeze(
  CHANNEL_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
);

export const CHANNEL_LEGACY_WEB_TO_CANONICAL = Object.freeze({
  'douyin_web': 'douyin',
  'xhs_web': 'xiaohongshu',
  'kuaishou_web': 'kuaishou',
  'xianyu_web': 'xianyu',
  'tiktok_web': 'tiktok',
  'xhs': 'xiaohongshu',
  'jd_web': 'jd',
  'jingdong': 'jd',
  'bili': 'bilibili',
  'bilibili_web': 'bilibili',
  'wb': 'weibo',
  'weibo_web': 'weibo',
  // CH-16 首切片（2026-11-09）：拼多多旧写法归一 canonical=pdd（与后端
  // bridge.NormalizeBridgeChannel 的 pinduoduo/pdd_web 别名一致）。
  'pdd_web': 'pdd',
  'pinduoduo': 'pdd',
  // 2026-11-18 电商店铺五渠道旧写法归一（与后端 bridge.NormalizeBridgeChannel 一致）。
  'taobao_web': 'taobao',
  'tmall': 'taobao',
  'qianniu': 'taobao',
  'douyin_shop_web': 'douyin_shop',
  'jinritemai': 'douyin_shop',
  'feige': 'douyin_shop',
  'kuaishou_shop_web': 'kuaishou_shop',
  'kwaixiaodian': 'kuaishou_shop',
  'kuaishouxiao': 'kuaishou_shop',
  'shopee_web': 'shopee',
  'shopee_seller': 'shopee',
  'walmart': 'walmart_us',
  'walmart_web': 'walmart_us',
  'wx_personal': 'personal_wx',
  'wechat_personal': 'personal_wx'
});

export const CHANNEL_TAG_TYPE_MAP = Object.freeze(
  CHANNEL_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
);

export const CHANNEL_MAP = Object.freeze(
  CHANNEL_OPTIONS.reduce((acc, o) => { acc[o.value] = o; return acc }, {})
);

export const getChannelLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  if (CHANNEL_LEGACY_WEB_TO_CANONICAL[value])
    return getChannelLabel(CHANNEL_LEGACY_WEB_TO_CANONICAL[value]);
  if (CHANNEL_LABEL_MAP[value]) return CHANNEL_LABEL_MAP[value]
  if (CHANNEL_LABEL_ALIAS[value])
    return CHANNEL_LABEL_ALIAS[value];
  return value
};

// tag 颜色跟 canonical 渠道走，不跟落库写法走：公众号入站写 wechat、触达写 weixin，
// 两批行显示同名「微信公众号」就该同色（都 success）；旧网页别名（wx_personal 等）
// 同理归到所属渠道。认不出的值仍落 info 灰（未知兜底不变），空值也照旧 info。
export const getChannelTagType = (value) => {
  if (CHANNEL_LEGACY_WEB_TO_CANONICAL[value])
    return getChannelTagType(CHANNEL_LEGACY_WEB_TO_CANONICAL[value]);
  if (CHANNEL_TAG_TYPE_MAP[value]) return CHANNEL_TAG_TYPE_MAP[value]
  const aliasOption = CHANNEL_OPTIONS.find((o) => o.alias && o.alias.includes(value))
  return aliasOption ? aliasOption.tagType : 'info'
};

export const getChannelOption = (value) => CHANNEL_MAP[value] || null;

export const filterChannelsByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return CHANNEL_OPTIONS.filter((o) => list.includes(o.group))
};

export const excludeChannels = (excludeValues = []) => {
  const set = new Set(excludeValues)
  return CHANNEL_OPTIONS.filter((o) => !set.has(o.value))
};

export const includeChannels = (includeValues = []) => {
  const set = new Set(includeValues)
  return CHANNEL_OPTIONS.filter((o) => set.has(o.value))
};

export default {
  CHANNEL_GROUP,
  CHANNEL_OPTIONS,
  CHANNEL_LABEL_MAP,
  CHANNEL_TAG_TYPE_MAP,
  CHANNEL_LABEL_ALIAS,
  CHANNEL_LEGACY_WEB_TO_CANONICAL,
  CHANNEL_MAP,
  getChannelLabel,
  getChannelTagType,
  getChannelOption,
  filterChannelsByGroup,
  excludeChannels,
  includeChannels,
  PLATFORM_GROUP_MEMBERS,
  PLATFORM_GROUP_MEMBERS_REVERSE
}
