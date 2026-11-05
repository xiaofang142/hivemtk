<template>
  <el-alert
    v-if="loaded && engines.length"
    class="probe-mode-banner"
    :type="realCount > 0 ? 'success' : 'warning'"
    :closable="false"
    show-icon
  >
    <template #title>
      <span>数据来源：</span>
      <el-tag
        v-for="e in engines"
        :key="e.name"
        size="small"
        :type="e.mode === 'real' ? 'success' : e.mode === 'manual' ? 'info' : 'warning'"
        style="margin-right: 6px"
      >
        {{ e.name }} · {{ modeText(e.mode) }}
      </el-tag>
      <span v-if="realCount === 0" class="muted">
        当前全部为模拟探针（LLM 扮演 AI 搜索），不能当作真实 AI 引擎读数。
      </span>
    </template>
  </el-alert>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { listProbeEngineMeta } from '@/api/geoProbe'

const engines = ref([])
const loaded = ref(false)
const realCount = ref(0)

const modeText = (mode) => {
  if (mode === 'real') return '真实'
  if (mode === 'manual') return '人工导入'
  return '模拟'
}

onMounted(async () => {
  try {
    const list = await listProbeEngineMeta()
    engines.value = Array.isArray(list) ? list : []
    realCount.value = engines.value.filter((e) => e.mode === 'real').length
  } catch {
    engines.value = []
  } finally {
    loaded.value = true
  }
})
</script>

<style scoped>
.probe-mode-banner {
  margin-bottom: 12px;
}
.muted {
  color: #92400e;
  margin-left: 4px;
}
</style>
