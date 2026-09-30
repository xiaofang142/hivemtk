<template>
  <div class="persona-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('销冠能力画像') }}</h2>
        <p class="subtitle">5 维度能力评估 · 趋势追踪 · 团队对比分析</p>
      </div>
      <el-button type="primary" @click="loadStaffs">
        <el-icon><Refresh /></el-icon>
        {{ $t('刷新') }}
      </el-button>
    </el-card>

    <el-row :gutter="20">
      <el-col :span="8">
        <el-card class="staff-card">
          <template #header>
            <div class="card-header">
              <span>{{ $t('员工列表') }}</span>
              <el-input
                v-model="search"
                size="small"
                :placeholder="$t('搜索姓名/ID')"
                style="width: 180px"
                clearable
              />
            </div>
          </template>
          <el-table
            :data="filteredStaffs"
            v-loading="loadingStaffs"
            highlight-current-row
            @row-click="selectStaff"
            stripe
            height="500"
          >
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column prop="name" label="姓名" min-width="120" />
            <el-table-column label="综合分" width="100" align="center">
              <template #default="{ row }">
                <el-tag :type="scoreTagType(row.overall_score || row.overallScore)">
                  {{ Math.round(row.overall_score || row.overallScore || 0) }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :span="16">
        <el-card v-if="currentStaff" class="detail-card">
          <template #header>
            <div class="card-header">
              <div>
                <h3 style="margin: 0">{{ currentStaff.staff_name || currentStaff.name }}</h3>
                <span class="subtitle">员工 ID: {{ currentStaff.staff_id || currentStaff.id }}</span>
              </div>
              <el-button-group>
                <el-button :type="compareMode ? 'primary' : ''" @click="compareMode = !compareMode">
                  <el-icon><DataLine /></el-icon>
                  {{ compareMode ? '退出对比' : '对比模式' }}
                </el-button>
                <el-button :disabled="!(currentStaff?.staff_id || currentStaff?.id)" @click="loadReport">
                  <el-icon><Refresh /></el-icon>
                  重新生成
                </el-button>
              </el-button-group>
            </div>
          </template>

          <div v-if="loadingReport" v-loading="true" class="report-loading"></div>
          <template v-else-if="currentReport">
            <el-row :gutter="20" class="overview-row">
              <el-col :span="8">
                <div class="overview-card">
                  <div class="overview-label">综合评分</div>
                  <div class="overview-value" :style="{ color: scoreColorValue(currentReport.overall_score) }">
                    {{ Math.round(currentReport.overall_score || 0) }}
                  </div>
                  <div class="overview-tip">满分 100</div>
                </div>
              </el-col>
              <el-col :span="8">
                <div class="overview-card">
                  <div class="overview-label">评估维度</div>
                  <div class="overview-value">{{ (currentReport.items || []).length }}</div>
                  <div class="overview-tip">个能力维度</div>
                </div>
              </el-col>
              <el-col :span="8">
                <div class="overview-card">
                  <div class="overview-label">生成时间</div>
                  <div class="overview-value time-text">
                    {{ formatTime(currentReport.generated_at) }}
                  </div>
                  <div class="overview-tip">最近一次评估</div>
                </div>
              </el-col>
            </el-row>

            <div class="radar-section">
              <h4>能力雷达图</h4>
              <div class="radar-chart">
                <svg :viewBox="`0 0 ${svgSize} ${svgSize}`" width="100%" :style="{ maxWidth: svgSize + 'px', margin: '0 auto', display: 'block' }">
                  
                  <g v-for="(ring, idx) in [0.2, 0.4, 0.6, 0.8, 1.0]" :key="`ring-${idx}`">
                    <polygon
                      :points="radarPoints(ring)"
                      fill="none"
                      stroke="#dcdfe6"
                      stroke-width="1"
                    />
                  </g>
                  
                  <g v-for="(item, idx) in currentReport.items || []" :key="`axis-${idx}`">
                    <line
                      :x1="svgCenter"
                      :y1="svgCenter"
                      :x2="axisX(idx)"
                      :y2="axisY(idx)"
                      stroke="#dcdfe6"
                      stroke-width="1"
                    />
                    <text
                      :x="labelX(idx)"
                      :y="labelY(idx)"
                      text-anchor="middle"
                      font-size="14"
                      fill="#606266"
                    >{{ item.name }}</text>
                  </g>
                  
                  <polygon
                    :points="scorePoints"
                    fill="rgba(64, 158, 255, 0.3)"
                    stroke="#4F46E5"
                    stroke-width="2"
                  />
                  
                  <g v-for="(item, idx) in currentReport.items || []" :key="`pt-${idx}`">
                    <circle
                      :cx="scoreX(idx)"
                      :cy="scoreY(idx)"
                      r="4"
                      fill="#4F46E5"
                    />
                  </g>
                </svg>
              </div>
            </div>

            <div class="items-section">
              <h4>能力维度详情</h4>
              <el-table :data="currentReport.items || []" stripe>
                <el-table-column prop="name" label="能力维度" min-width="140" />
                <el-table-column prop="tag" label="标签" width="120">
                  <template #default="{ row }">
                    <el-tag size="small">{{ row.tag }}</el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="得分" width="200">
                  <template #default="{ row }">
                    <el-progress
                      :percentage="Math.round(row.score || 0)"
                      :stroke-width="12"
                      :color="scoreColor(row.score)"
                    />
                  </template>
                </el-table-column>
                <el-table-column prop="sample" label="样本数" width="100" align="center" />
                <el-table-column label="趋势" width="100" align="center">
                  <template #default="{ row }">
                    <el-tag v-if="row.trend === 'up'" type="success" size="small">
                      <el-icon><CaretTop /></el-icon>{{ getTrendLabel(row.trend) }}
                    </el-tag>
                    <el-tag v-else-if="row.trend === 'down'" type="danger" size="small">
                      <el-icon><CaretBottom /></el-icon>{{ getTrendLabel(row.trend) }}
                    </el-tag>
                    <el-tag v-else type="info" size="small">
                      <el-icon><Minus /></el-icon>{{ getTrendLabel(row.trend) }}
                    </el-tag>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            
            <div v-if="compareMode" class="compare-section">
              <h4>员工对比</h4>
              <el-select
                v-model="compareStaffId"
                placeholder="选择对比员工"
                filterable
                clearable
                style="width: 240px; margin-bottom: 15px"
                @change="loadCompareReport"
              >
                <el-option
                  v-for="s in otherStaffs"
                  :key="s.id"
                  :label="`${s.name} (${Math.round(s.overall_score || s.overallScore || 0)}分)`"
                  :value="s.id"
                />
              </el-select>
              <div v-if="compareReport" class="compare-chart">
                <svg :viewBox="`0 0 ${svgSize} ${svgSize}`" width="100%" :style="{ maxWidth: svgSize + 'px', margin: '0 auto', display: 'block' }">
                  <g v-for="(ring, idx) in [0.2, 0.4, 0.6, 0.8, 1.0]" :key="`cring-${idx}`">
                    <polygon
                      :points="radarPoints(ring)"
                      fill="none"
                      stroke="#dcdfe6"
                      stroke-width="1"
                    />
                  </g>
                  <g v-for="(item, idx) in compareReport.items || []" :key="`caxis-${idx}`">
                    <line
                      :x1="svgCenter"
                      :y1="svgCenter"
                      :x2="axisX(idx)"
                      :y2="axisY(idx)"
                      stroke="#dcdfe6"
                      stroke-width="1"
                    />
                    <text
                      :x="labelX(idx)"
                      :y="labelY(idx)"
                      text-anchor="middle"
                      font-size="14"
                      fill="#606266"
                    >{{ item.name }}</text>
                  </g>
                  
                  <polygon
                    :points="scorePoints"
                    fill="rgba(64, 158, 255, 0.2)"
                    stroke="#4F46E5"
                    stroke-width="2"
                  />
                  
                  <polygon
                    :points="comparePoints"
                    fill="rgba(230, 162, 60, 0.2)"
                    stroke="#F59E0B"
                    stroke-width="2"
                    stroke-dasharray="4 2"
                  />
                </svg>
                <div class="compare-legend">
                  <span><span class="legend-dot" style="background: #4F46E5"></span>当前员工</span>
                  <span><span class="legend-dot" style="background: #F59E0B"></span>对比员工</span>
                </div>
              </div>
            </div>
          </template>
          <el-empty v-else description="请选择员工查看画像" />
        </el-card>
        <el-card v-else>
          <el-empty description="请从左侧选择员工" />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, DataLine, CaretTop, CaretBottom, Minus } from '@element-plus/icons-vue'
import { listStaffs, getPersonaReport } from '@/api/persona.js'
import { getTrendLabel } from '@/constants/trend';

const loadingStaffs = ref(false)
const loadingReport = ref(false)
const staffs = ref([])
const currentStaff = ref(null)
const currentReport = ref(null)
const search = ref('')
const compareMode = ref(false)
const compareStaffId = ref(null)
const compareReport = ref(null)

const svgSize = 400;
const svgCenter = svgSize / 2
const svgRadius = 150

const filteredStaffs = computed(() => {
  if (!search.value) return staffs.value
  const kw = search.value.toLowerCase()
  return staffs.value.filter(
    (s) => String(s.id).includes(kw) || s.name?.toLowerCase().includes(kw)
  )
})

const otherStaffs = computed(() => {
  const cur = currentStaff.value?.staff_id || currentStaff.value?.id
  return staffs.value.filter((s) => (s.id || s.staff_id) !== cur)
})

const formatTime = (val) => {
  if (!val) return '-'
  const d = new Date(val)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}

const scoreColor = (score) => {
  if (score >= 80) return '#10B981'
  if (score >= 60) return '#F59E0B'
  return '#EF4444'
}

// 综合分对应的 el-tag 语义色。必须与 scoreColor 分开：
// el-tag 的 type 只能取 primary/success/info/warning/danger，直接传 scoreColor 的 hex
// （实测返回 '#EF4444'）会触发 "Invalid prop: validation failed for prop type" 告警，
// 标签色还会错误回落到 primary 蓝色。scoreColor 仍用于 :color 进度条与 scoreColorValue 文字色。
const scoreTagType = (score) => {
  if (score >= 80) return 'success'
  if (score >= 60) return 'warning'
  return 'danger'
}

const scoreColorValue = (score) => scoreColor(score)

const loadStaffs = async () => {
  loadingStaffs.value = true
  try {
    const res = await listStaffs()
    const data = res || []
    const list = Array.isArray(data) ? data : data.list || []
    staffs.value = list
    if (list.length > 0 && !currentStaff.value) {
      selectStaff(list[0])
    }
  } catch (e) {
    ElMessage.error(i18n.global.t('加载员工列表失败'))
    staffs.value = []
  } finally {
    loadingStaffs.value = false
  }
}

const selectStaff = (row) => {
  currentStaff.value = row
  currentReport.value = null
  compareReport.value = null
  compareStaffId.value = null
  loadReport()
}

const loadReport = async () => {
  if (!currentStaff.value) return
  loadingReport.value = true
  try {
    const id = currentStaff.value.staff_id || currentStaff.value.id
    const res = await getPersonaReport(id)
    currentReport.value = res
  } catch (e) {
    ElMessage.error('画像生成失败：' + (e?.message || ''))
    currentReport.value = null;
  } finally {
    loadingReport.value = false
  }
}

const loadCompareReport = async () => {
  if (!compareStaffId.value) {
    compareReport.value = null
    return
  }
  try {
    const res = await getPersonaReport(compareStaffId.value)
    compareReport.value = res
  } catch (e) {
    ElMessage.error(i18n.global.t('对比员工画像加载失败'))
    compareReport.value = null
  }
}

const itemsCount = computed(() => (currentReport.value?.items || []).length || 5);

const angle = (idx) => (Math.PI * 2 * idx) / itemsCount.value - Math.PI / 2

const axisX = (idx) => svgCenter + Math.cos(angle(idx)) * svgRadius
const axisY = (idx) => svgCenter + Math.sin(angle(idx)) * svgRadius
const labelX = (idx) => svgCenter + Math.cos(angle(idx)) * (svgRadius + 25)
const labelY = (idx) => svgCenter + Math.sin(angle(idx)) * (svgRadius + 25)

const radarPoints = (scale) => {
  const n = itemsCount.value
  const points = []
  for (let i = 0; i < n; i++) {
    const x = svgCenter + Math.cos(angle(i)) * svgRadius * scale
    const y = svgCenter + Math.sin(angle(i)) * svgRadius * scale
    points.push(`${x},${y}`)
  }
  return points.join(' ')
}

const scorePoints = computed(() => {
  const items = currentReport.value?.items || []
  if (items.length === 0) return ''
  const points = []
  for (let i = 0; i < items.length; i++) {
    const x = svgCenter + Math.cos(angle(i)) * svgRadius * ((items[i].score || 0) / 100)
    const y = svgCenter + Math.sin(angle(i)) * svgRadius * ((items[i].score || 0) / 100)
    points.push(`${x},${y}`)
  }
  return points.join(' ')
})

const scoreX = (idx) => {
  const items = currentReport.value?.items || []
  return svgCenter + Math.cos(angle(idx)) * svgRadius * ((items[idx]?.score || 0) / 100)
}
const scoreY = (idx) => {
  const items = currentReport.value?.items || []
  return svgCenter + Math.sin(angle(idx)) * svgRadius * ((items[idx]?.score || 0) / 100)
}

const comparePoints = computed(() => {
  const items = compareReport.value?.items || []
  if (items.length === 0) return ''
  const points = []
  for (let i = 0; i < items.length; i++) {
    const x = svgCenter + Math.cos(angle(i)) * svgRadius * ((items[i].score || 0) / 100)
    const y = svgCenter + Math.sin(angle(i)) * svgRadius * ((items[i].score || 0) / 100)
    points.push(`${x},${y}`)
  }
  return points.join(' ')
})

onMounted(() => {
  loadStaffs()
})
</script>

<style scoped lang="scss">
.persona-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; font-size: 12px; }
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.staff-card, .detail-card { height: 100%; }
.overview-row { margin-bottom: 20px; }
.overview-card {
  text-align: center;
  padding: 20px 10px;
  background: #f5f7fa;
  border-radius: 6px;
  .overview-label { color: #909399; font-size: 13px; margin-bottom: 8px; }
  .overview-value { font-size: 32px; font-weight: bold; line-height: 1.2; }
  .overview-tip { color: #c0c4cc; font-size: 11px; margin-top: 6px; }
  .time-text { font-size: 14px; }
}
.radar-section, .items-section, .compare-section {
  margin-top: 20px;
  h4 { margin: 0 0 12px 0; color: #303133; }
}
.radar-chart {
  background: #fafafa;
  border-radius: 6px;
  padding: 20px;
}
.compare-legend {
  display: flex;
  justify-content: center;
  gap: 30px;
  margin-top: 10px;
  font-size: 13px;
  .legend-dot {
    display: inline-block;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    margin-right: 6px;
    vertical-align: middle;
  }
}
.report-loading { min-height: 400px; }
</style>
