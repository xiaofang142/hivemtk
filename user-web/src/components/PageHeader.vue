<template>
  <header
    class="page-header"
    :aria-label="ariaLabel || $t('common.pageHeader') || '页面头部'"
  >
    <div class="ph-left">
      <div class="ph-icon" v-if="icon || $slots.icon" aria-hidden="true">
        <el-icon v-if="icon"><component :is="icon" /></el-icon>
        <slot name="icon" />
      </div>
      <div class="ph-text">
        <h2 class="ph-title" :id="titleId">{{ title }}</h2>
        <p class="ph-subtitle" v-if="subtitle" :id="subtitleId">{{ subtitle }}</p>
      </div>
    </div>
    <div class="ph-actions" role="group" :aria-label="ariaLabel || '页面操作'">
      <slot />
    </div>
  </header>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  title: { type: String, default: '' },
  subtitle: { type: String, default: '' },
  icon: { type: [String, Object], default: '' },
  ariaLabel: { type: String, default: '' },
})

const titleId = computed(() => `ph-title-${Math.random().toString(36).slice(2, 9)}`)
const subtitleId = computed(() => `ph-sub-${titleId.value}`)
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}
.ph-left {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}
.ph-icon {
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  background: linear-gradient(135deg, #EEF2FF, #E0E7FF);
  color: #4F46E5;
  font-size: 22px;
}
.ph-title {
  font-size: 20px;
  font-weight: 600;
  color: #0F172A;
  margin: 0;
  line-height: 1.3;
}
.ph-subtitle {
  font-size: 13px;
  color: #64748B;
  margin: 4px 0 0;
  line-height: 1.5;
}
.ph-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
</style>
