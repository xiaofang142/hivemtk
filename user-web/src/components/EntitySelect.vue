<template>
  <el-select
    :model-value="modelValue"
    :placeholder="placeholder"
    :clearable="clearable"
    :filterable="filterable"
    :multiple="multiple"
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
// 外键字段统一下拉：entity=user|adminUser|agent|customer。
// 值是关联实体 id（原类型直传），label 是「名称（#id）」，与列表关联值同口径。
import { useEntityOptions } from '@/composables/useEntityOptions'

const props = defineProps({
  modelValue: { type: [String, Number, Array], default: null },
  entity: { type: String, required: true },
  placeholder: { type: String, default: '请选择' },
  clearable: { type: Boolean, default: true },
  filterable: { type: Boolean, default: true },
  multiple: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  admin: { type: Boolean, default: false },
  size: { type: String, default: 'default' }
})
const emit = defineEmits(['update:modelValue', 'change'])
const { options } = useEntityOptions(props.entity, { admin: props.admin })

function onChange(value) {
  emit('update:modelValue', value)
  emit('change', value)
}
</script>
