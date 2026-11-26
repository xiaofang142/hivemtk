<template>
  <el-select
    :model-value="modelValue"
    :placeholder="placeholder"
    :clearable="clearable"
    filterable
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
// 全系统唯一的渠道账号下拉源（027 渠道统一化）：只列已启用账号，
// 且所属渠道被停用时 fail-closed 不返回。value 为账号数字 ID，与业务表 account_id 同口径。
import { computed, onMounted } from 'vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'

const props = defineProps({
  modelValue: { type: [String, Number], default: null },
  // 传渠道规范码或数字 ID 可把候选收敛到单个渠道下。
  channel: { type: [String, Number], default: null },
  placeholder: { type: String, default: '请选择账号' },
  clearable: { type: Boolean, default: true },
  disabled: { type: Boolean, default: false },
  size: { type: String, default: 'default' }
})
const emit = defineEmits(['update:modelValue', 'change'])

const catalog = useChannelCatalogStore()
onMounted(() => catalog.ensure())

const options = computed(() => catalog.accountOptions(props.channel))

function onChange(value) {
  emit('update:modelValue', value)
  emit('change', value)
}
</script>
