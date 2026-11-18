<template>
  <div class="rfm-matrix">
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            总用户
          </div>
          <div class="stat-value">
            {{ stats.total }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">
            高价值
          </div>
          <div class="stat-value">
            {{ stats.highValue }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card primary">
          <div class="stat-label">
            活跃
          </div>
          <div class="stat-value">
            {{ stats.active }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card danger">
          <div class="stat-label">
            流失风险
          </div>
          <div class="stat-value">
            {{ stats.churnRisk }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>RFM 矩阵（Recency × Frequency）</span>
          <el-button-group>
            <el-button @click="exportToReach">
              导出到触达
            </el-button>
            <el-button
              type="primary"
              @click="saveSegment"
            >
              保存为分群
            </el-button>
          </el-button-group>
        </div>
      </template>
      <div
        ref="matrixRef"
        class="matrix"
      />
      <el-table
        :data="segments"
        class="segments-table"
      >
        <el-table-column
          prop="name"
          label="分群名称"
        />
        <el-table-column
          prop="recency"
          label="R"
          width="80"
        />
        <el-table-column
          prop="frequency"
          label="F"
          width="80"
        />
        <el-table-column
          prop="monetary"
          label="M"
          width="100"
        />
        <el-table-column
          prop="count"
          label="人数"
          width="100"
        />
        <el-table-column
          prop="revenue"
          label="GMV"
          width="120"
        />
        <el-table-column
          label="操作"
          width="160"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="viewDetail(row)"
            >
              详情
            </el-button>
            <el-button
              link
              type="primary"
              @click="reachSegment(row)"
            >
              触达
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue';
import { ElMessage } from 'element-plus'
import * as _echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { http } from '@/utils/request'

const stats = reactive({ total: 0, highValue: 0, active: 0, churnRisk: 0 })
const segments = ref([])
const matrixRef = ref()

async function load() {
  const [s, sg] = await Promise.all([
    http.get('/api/user-segments/rfm/stats'),
    http.get('/api/user-segments/rfm')
  ])
  Object.assign(stats, s || {})
  segments.value = sg || []
  renderMatrix(sg || [])
}

function renderMatrix(data) {
  if (!matrixRef.value) return
  const chart = safeInit(matrixRef.value)
  chart.setOption({
    tooltip: {},
    xAxis: { type: 'category', data: ['R5', 'R4', 'R3', 'R2', 'R1'] },
    yAxis: { type: 'category', data: ['F1', 'F2', 'F3', 'F4', 'F5'] },
    visualMap: { min: 0, max: 1000, calculable: true, orient: 'vertical', left: 'right' },
    series: [{
      type: 'heatmap',
      data: data.map((d) => [5 - d.recency + 1, d.frequency - 1, d.count])
    }]
  })
}

const saving = ref(false);

async function saveSegment() {
  if (saving.value) return
  saving.value = true
  try {
    const hv = (segments.value || []).filter((d) => d.recency >= 4 && d.frequency >= 4);
    await http.post('/api/user-segments', {
      name: `RFM高价值客群 ${new Date().toLocaleDateString()}`,
      description: '由 RFM 矩阵保存：R>=4 且 F>=4 的客户桶',
      rules: { type: 'rfm_snapshot', matrix: hv, saved_at: new Date().toISOString() },
      trigger: 'static'
    })
    ElMessage.success('分群已保存（可在分群列表查看）')
  } catch (e) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function exportToReach() {
  window.open('/#/reachPipeline/list', '_blank');
}

function viewDetail(_row) {
  window.open('/#/customer360/list', '_blank');
}

function reachSegment(__row) {
  window.open('/#/reachPipeline/list', '_blank');
}

onMounted(load)
</script>

<style scoped>
.rfm-matrix { padding: 16px; }
.stats-row { margin-bottom: 16px; }
.stat-card { padding: 8px; text-align: center; }
.stat-label { color: #64748B; font-size: 12px; }
.stat-value { font-size: 32px; font-weight: 700; margin: 8px 0; }
.stat-card.success .stat-value { color: #10B981; }
.stat-card.primary .stat-value { color: #4F46E5; }
.stat-card.danger .stat-value { color: #EF4444; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.matrix { width: 100%; height: 320px; }
.segments-table { margin-top: 16px; }
</style>
