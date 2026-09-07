export const ASSET_TYPE_OPTIONS = Object.freeze([
  { value: 'agent_persona',      label: '智能体角色',  tagType: 'primary', icon: 'User',          description: 'AI 智能体角色（人设）' },
  { value: 'sales_script',       label: '销冠话术',    tagType: 'success', icon: 'ChatLineRound', description: '销售话术' },
  { value: 'ab_test_plan',       label: 'AB 测试',      tagType: 'warning', icon: 'DataAnalysis',  description: 'AB 实验方案' },
  { value: 'marketing_workflow', label: '工作流',      tagType: 'info',    icon: 'Share',         description: '营销工作流' },
  { value: 'industry_sop',       label: '行业 SOP',     tagType: 'info',        icon: 'Document',      description: '行业标准操作流程' },
  { value: 'knowledge_base',     label: '知识库',      tagType: 'primary', icon: 'Files',         description: '知识库' },
  { value: 'persona',            label: '角色',        tagType: 'info',    icon: 'UserFilled',    description: '角色（旧值）' }
]);

export const ASSET_TYPE_LABEL_MAP = Object.freeze(
  ASSET_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const ASSET_TYPE_TAG_TYPE_MAP = Object.freeze(
  ASSET_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getAssetTypeLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return ASSET_TYPE_LABEL_MAP[v] || String(v)
}

export const getAssetTypeTagType = (v) => ASSET_TYPE_TAG_TYPE_MAP[v] || 'info'

export default {
  ASSET_TYPE_OPTIONS,
  ASSET_TYPE_LABEL_MAP,
  ASSET_TYPE_TAG_TYPE_MAP,
  getAssetTypeLabel,
  getAssetTypeTagType
}
