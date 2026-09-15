<template>
  <div class="site-publish">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>📤 发布到官网</span>
        </div>
      </template>
      <el-steps :active="step" finish-status="success" simple style="margin-bottom:24px">
        <el-step title="1. 导出 Markdown" />
        <el-step title="2. 触发部署" />
        <el-step title="3. 蜘蛛推送" />
        <el-step title="4. 健康检查" />
      </el-steps>

      <el-button-group style="margin-bottom:16px">
        <el-button @click="exportOnly">仅导出</el-button>
        <el-button @click="deployOnly">仅部署</el-button>
        <el-button type="primary" @click="fullPipeline">🚀 一键全链路</el-button>
      </el-button-group>

      <el-alert v-if="lastResult" :title="lastResult" type="success" show-icon :closable="false" />
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { geoApi } from '@/api/geo'
import { ElMessage } from 'element-plus'
const step = ref(0)
const lastResult = ref('')
async function exportOnly() {
  try {
    const res = await geoApi.exportSite()
    step.value = 1
    lastResult.value = `导出 ${res?.exported ?? 0} 篇`
    ElMessage.success('导出成功')
  } catch (e) { ElMessage.error('导出失败') }
}
async function deployOnly() {
  try {
    const res = await geoApi.deploySite()
    step.value = 2
    lastResult.value = `部署地址：${res?.site_url || '—'}`
    ElMessage.success('部署成功')
  } catch (e) { ElMessage.error('部署失败') }
}
async function fullPipeline() {
  step.value = 1
  try {
    const res = await geoApi.siteFullPipeline()
    step.value = 4
    lastResult.value = `导出 ${res?.exported} 篇 → ${res?.site_url}`
    ElMessage.success('全链路完成！')
  } catch (e) { step.value = 0 }
}
</script>
