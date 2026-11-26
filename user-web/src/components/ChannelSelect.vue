<template>
  <el-select
    :model-value="modelValue"
    :placeholder="placeholder"
    :clearable="clearable"
    :filterable="filterable"
    :disabled="disabled"
    :size="size"
    @update:model-value="onChange"
  >
    <el-option
      v-for="opt in options"
      :key="opt.value"
      :label="opt.label"
      :value="opt.value"
    />
  </el-select>
</template>

<script setup>
// 全系统唯一的渠道下拉源（027 渠道统一化）：数据来自 /api/channels/catalog，
// 默认只列已开启渠道且不允许输入。includeDisabled 仅供管理页开关使用。
import { computed, onMounted } from 'vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const props = defineProps({
  modelValue: { type: [String, Number], default: null },
  placeholder: { type: String, default: '请选择渠道' },
  clearable: { type: Boolean, default: true },
  filterable: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  size: { type: String, default: 'default' },
  // 历史业务表存的是规范码；管理页按数字 id 操作时传 value-key="id"。
  valueKey: { type: String, default: 'code' },
  includeDisabled: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'change'])

const catalog = useChannelCatalogStore()
onMounted(() => catalog.ensure())

const options = computed(() => {
  const src = props.includeDisabled ? catalog.allChannels : catalog.channels
  return src.map((c) => ({
    value: props.valueKey === 'id' ? c.id : c.code,
    label: c.label || c.name || c.code,
    code: c.code,
    id: c.id
  }))
})

function onChange(value) {
  emit('update:modelValue', value)
  emit('change', value)
}
</script>
