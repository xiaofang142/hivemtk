export const CARD_PLATFORM = Object.freeze({
  DOUYIN: 'douyin',
  KUAISHOU: 'kuaishou',
  XIAOHONGSHU: 'xiaohongshu',
  XIANYU: 'xianyu',
  TIKTOK: 'tiktok'
});

export const CARD_PLATFORM_OPTIONS = Object.freeze([
  { value: 'douyin',       label: '抖音',   tagType: 'danger',        sort: 1, group: 'card',   icon: 'VideoCamera' },
  { value: 'kuaishou',     label: '快手',   tagType: 'warning',        sort: 2, group: 'card',   icon: 'VideoCamera' },
  { value: 'xiaohongshu',  label: '小红书', tagType: 'danger',  sort: 3, group: 'card',   icon: 'Postcard' },
  { value: 'xianyu',       label: '闲鱼',   tagType: 'warning', sort: 4, group: 'card',   icon: 'Goods' },
  { value: 'tiktok',       label: 'TikTok', tagType: 'danger',        sort: 5, group: 'card',   icon: 'VideoCamera' }
]);

export const CLUE_TYPE_OPTIONS = Object.freeze([
  { value: '1', label: '小红书', tagType: 'danger',  sort: 1, group: 'clue' },
  { value: '2', label: '视频号', tagType: 'success', sort: 2, group: 'clue' },
  { value: '3', label: '抖音',   tagType: 'info',        sort: 3, group: 'clue' },
  { value: '4', label: '快手',   tagType: 'info',        sort: 4, group: 'clue' }
]);

export const CLUE_TYPE_OPTIONS_LEGACY = Object.freeze([
  { value: '1', label: 'QQ',         tagType: 'info', sort: 1, group: 'legacy' },
  { value: '2', label: '微信',       tagType: 'success', sort: 2, group: 'legacy' },
  { value: '3', label: '电话',       tagType: 'warning', sort: 3, group: 'legacy' },
  { value: '4', label: 'Telegram',   tagType: 'primary', sort: 4, group: 'legacy' },
  { value: '5', label: 'Whatsapp',   tagType: 'success', sort: 5, group: 'legacy' },
  { value: '6', label: 'twitter',    tagType: 'info', sort: 6, group: 'legacy' }
]);

const _buildLabelMap = (arr) => arr.reduce((acc, o) => {
  acc[o.value] = o.label
  acc[String(o.value)] = o.label
  return acc
}, {});
const _buildTagTypeMap = (arr) => arr.reduce((acc, o) => {
  acc[o.value] = o.tagType
  acc[String(o.value)] = o.tagType
  return acc
}, {})

export const CARD_PLATFORM_LABEL_MAP = Object.freeze(_buildLabelMap(CARD_PLATFORM_OPTIONS))
export const CARD_PLATFORM_TAG_TYPE_MAP = Object.freeze(_buildTagTypeMap(CARD_PLATFORM_OPTIONS))
export const CLUE_TYPE_LABEL_MAP = Object.freeze({
  ..._buildLabelMap(CLUE_TYPE_OPTIONS),
  ..._buildLabelMap(CLUE_TYPE_OPTIONS_LEGACY)
})

export const getCardPlatformLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return CARD_PLATFORM_LABEL_MAP[value]
    || CLUE_TYPE_LABEL_MAP[value]
    || String(value)
};

export const getCardPlatformTagType = (value) => {
  if (value === undefined || value === null || value === '') return ''
  return CARD_PLATFORM_TAG_TYPE_MAP[value] || CLUE_TYPE_LABEL_MAP[value] || 'info'
};

export const getClueTypeLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return CLUE_TYPE_LABEL_MAP[value] || CARD_PLATFORM_LABEL_MAP[value] || String(value)
};

export const getClueTypeOptions = () => CLUE_TYPE_OPTIONS;

export const filterCardPlatformByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return [
    ...CARD_PLATFORM_OPTIONS,
    ...CLUE_TYPE_OPTIONS,
    ...CLUE_TYPE_OPTIONS_LEGACY
  ].filter((o) => list.includes(o.group))
};

export default {
  CARD_PLATFORM,
  CARD_PLATFORM_OPTIONS,
  CLUE_TYPE_OPTIONS,
  CLUE_TYPE_OPTIONS_LEGACY,
  CARD_PLATFORM_LABEL_MAP,
  CARD_PLATFORM_TAG_TYPE_MAP,
  CLUE_TYPE_LABEL_MAP,
  getCardPlatformLabel,
  getCardPlatformTagType,
  getClueTypeLabel,
  getClueTypeOptions,
  filterCardPlatformByGroup
}
