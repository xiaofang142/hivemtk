<template>
  <div class="index-tracking">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>🔍 收录与 AI 引用追踪</span>
          <div>
            <el-button type="primary" @click="load">刷新</el-button>
            <el-button @click="verifyAll">一键全量验证</el-button>
          </div>
        </div>
      </template>
      <el-empty v-if="!trackings.length" description="暂无收录数据" />
      <el-table v-else :data="trackings" size="small" border>
        <el-table-column prop="url" label="URL" show-overflow-tooltip />
        <el-table-column prop="engine" label="引擎" width="100" />
        <el-table-column label="收录" width="80">
          <template #default="{ row }">
            <el-tag :type="row.indexed ? 'success' : 'info'" size="small">{{ row.indexed ? '✅' : '—' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="rank_position" label="排名" width="80" />
        <el-table-column label="AI 引用" width="100">
          <template #default="{ row }">
            <el-tag :type="row.ai_cited ? 'success' : 'warning'" size="small">
              {{ row.ai_cited ? '✅ ' + row.ai_cite_count + '次' : '❌' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="last_checked" label="验证时间" width="160" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import request from '@/api/index'
import { ElMessage } from 'element-plus'
const trackings = ref([])

async function load() {
  try {
    const res = await request.get('/geo/index-tracking/funnel')
    trackings.value = res.data?.trackings || []
  } catch (e) {}
}
async function verifyAll() {
  try {
    await request.post('/geo/index-tracking/verify-all')
    ElMessage.success('已触发全量验证')
  } catch (e) { ElMessage.error('失败') }
}
onMounted(load)
</script>
