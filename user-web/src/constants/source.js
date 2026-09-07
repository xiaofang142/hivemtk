export const SOURCE_OPTIONS = Object.freeze([
  {
    value: 'auto',
    label: '系统自动',
    tagType: 'warning',
    group: 'common',
    description: '系统自动生成/触发'
  },
  { value: 'manual',    label: '手动',       tagType: 'info',    group: 'common', description: '人工操作产生' },
  { value: 'system',    label: '系统',       tagType: 'info',    group: 'common', description: '系统内部事件' },
  { value: 'rule',      label: '规则',       tagType: 'info',        group: 'common', description: '规则引擎生成' },
  { value: 'llm',       label: 'AI',         tagType: 'primary', group: 'common', description: 'AI/LLM 生成' },

  {
    value: 'upload',
    label: '文件上传',
    tagType: 'info',
    group: 'knowledge',
    description: '本地文件上传'
  },
  { value: 'text',      label: '文本输入',   tagType: 'success', group: 'knowledge', description: '在线文本输入' },
  { value: 'url',       label: 'URL 抓取',   tagType: 'warning', group: 'knowledge', description: 'URL 网页抓取' },
  { value: 'openapi',   label: 'OpenAPI',    tagType: 'info',    group: 'knowledge', description: '外部 OpenAPI 同步' },
  { value: 'batch',     label: '批量导入',   tagType: 'info',        group: 'knowledge', description: '批量任务导入' },
  { value: 'api',       label: 'API',        tagType: 'info',    group: 'knowledge', description: '外部 API 推送' },
  { value: 'crawler',   label: '爬虫',       tagType: 'warning', group: 'knowledge', description: '爬虫采集' },

  {
    value: 'import',
    label: '导入',
    tagType: 'info',
    group: 'batch',
    description: '批量导入'
  },
  { value: 'webhook',   label: 'Webhook',    tagType: 'success', group: 'batch', description: 'Webhook 回调' },

  {
    value: 'purchased',
    label: '平台购买',
    tagType: 'success',
    group: 'asset',
    description: '从平台购买'
  },
  { value: 'synced',    label: '平台分发',   tagType: 'success', group: 'asset', description: '平台同步分发' },
  { value: 'imported',  label: '导入',       tagType: 'info',    group: 'asset', description: '本地导入' }
]);

export const SOURCE_LABEL_MAP = Object.freeze(
  SOURCE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
);

export const SOURCE_TAG_TYPE_MAP = Object.freeze(
  SOURCE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
);

export const getSourceLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return SOURCE_LABEL_MAP[value] || value
};

export const getSourceTagType = (value) => SOURCE_TAG_TYPE_MAP[value] || 'info';

export const filterSourcesByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return SOURCE_OPTIONS.filter((o) => list.includes(o.group))
};

export default {
  SOURCE_OPTIONS,
  SOURCE_LABEL_MAP,
  SOURCE_TAG_TYPE_MAP,
  getSourceLabel,
  getSourceTagType,
  filterSourcesByGroup
}
