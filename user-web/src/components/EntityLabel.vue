<template>
  <span class="entity-label">{{ text }}</span>
</template>

<script setup>
// 外键列表列统一关联渲染：显示「名称（#id）」；历史裸值/无权限时回退「#id」，
// 原值不丢，仍可按 id 检索。
import { computed } from 'vue'
import { useEntityOptions } from '@/composables/useEntityOptions'

const props = defineProps({
  entity: { type: String, required: true },
  value: { type: [String, Number], default: null },
  admin: { type: Boolean, default: false },
  emptyText: { type: String, default: '-' }
})
const { labelOf } = useEntityOptions(props.entity, { admin: props.admin })
const text = computed(() => labelOf(props.value) || props.emptyText)
</script>

<style scoped>
.entity-label {
  white-space: nowrap;
}
</style>
