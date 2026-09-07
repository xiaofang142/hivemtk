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
  { value: 'weixin',        label: '微信公众号', tagType: 'success', group: CHANNEL_GROUP.IM,   icon: 'ChatDotRound',    description: '微信公众号（客服消息）', alias: ['微信'] },
  { value: 'personal_wx',   label: '个人微信',   tagType: 'success', group: CHANNEL_GROUP.SOCIAL, icon: 'ChatDotRound',  description: '个人微信（聚合）' },
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
  { value: 'kuaishou',      label: '快手',      tagType: 'info',        group: CHANNEL_GROUP.SOCIAL, icon: 'Share',           description: '快手私信' },
  { value: 'xiaohongshu',   label: '小红书',    tagType: 'danger',  group: CHANNEL_GROUP.SOCIAL, icon: 'Postcard',        description: '小红书私信' },
  { value: 'xianyu',        label: '闲鱼',      tagType: 'warning', group: CHANNEL_GROUP.SOCIAL, icon: 'Goods',           description: '闲鱼私信' },
  { value: 'tiktok',        label: 'TikTok',    tagType: 'info',        group: CHANNEL_GROUP.SOCIAL, icon: 'VideoCamera',     description: 'TikTok 私信' },
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
  xianyu: ['xianyu', 'xianyu_web']
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
  'xhs': 'xiaohongshu'
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

export const getChannelTagType = (value) => CHANNEL_TAG_TYPE_MAP[value] || 'info';

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
