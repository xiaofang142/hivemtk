<template>
  <div class="journey-dashboard">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('客户旅程大屏') }}</h2>
        <p class="subtitle">9 阶段实时监控 · 转化漏斗可视化 · 沉睡客户自动检测</p>
      </div>
      <div class="header-actions">
        <el-tag size="large" type="info" effect="plain">
          最后更新: {{ lastUpdate }}
        </el-tag>
        <el-switch
          v-model="autoRefresh"
          active-text="自动刷新"
          inactive-text="手动"
          @change="toggleAutoRefresh"
        />
        <el-button type="primary" :icon="Refresh" @click="loadOverview" :loading="loading">
          {{ $t('刷新') }}
        </el-button>
      </div>
    </el-card>

    
    <el-row :gutter="20" class="stat-row">
      <el-col :span="6">
        <el-card class="stat-card stat-blue">
          <div class="stat-icon"><el-icon><User /></el-icon></div>
          <div class="stat-content">
            <div class="stat-label">{{ $t('客户总数') }}</div>
            <div class="stat-value">{{ overview?.total_customers || 0 }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card stat-green">
          <div class="stat-icon"><el-icon><CircleCheck /></el-icon></div>
          <div class="stat-content">
            <div class="stat-label">{{ $t('成交客户') }}</div>
            <div class="stat-value">{{ wonCount }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card stat-orange">
          <div class="stat-icon"><el-icon><Sunny /></el-icon></div>
          <div class="stat-content">
            <div class="stat-label">{{ $t('意向客户') }}</div>
            <div class="stat-value">{{ interestedCount }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card stat-red">
          <div class="stat-icon"><el-icon><Moon /></el-icon></div>
          <div class="stat-content">
            <div class="stat-label">{{ $t('沉睡客户') }}</div>
            <div class="stat-value">{{ sleepingCount }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="funnel-card">
      <template #header>
        <div class="card-header">
          <span>
            <el-icon style="color: #4F46E5"><DataLine /></el-icon>
            {{ $t('客户旅程漏斗') }}
          </span>
          <span class="card-tip">{{ $t('从陌生到成交的全流程') }}</span>
        </div>
      </template>

      <div v-loading="loading" class="funnel-container">
        <div
          v-for="(stage, idx) in stageList"
          :key="stage.stage"
          class="funnel-stage"
          role="button"
          tabindex="0"
          :style="getFunnelStyle(idx)"
          @click="selectStage(stage)"
          @keydown.enter.prevent="selectStage(stage)"
          @keydown.space.prevent="selectStage(stage)"
        >
          <div class="funnel-stage-content">
            <div class="funnel-stage-label">{{ stage.label }}</div>
            <div class="funnel-stage-count">{{ stage.count }} 人</div>
            <div class="funnel-stage-rate">
              {{ stage.rate.toFixed(1) }}% · 平均停留 {{ stage.avg_stay_hours.toFixed(0) }}h
            </div>
          </div>
          <div v-if="idx < stageList.length - 1" class="funnel-arrow">↓</div>
        </div>
      </div>
    </el-card>

    
    <el-row :gutter="20" class="detail-row">
      <el-col :span="12">
        <el-card class="detail-card">
          <template #header>
            <div class="card-header">
              <span>阶段配置</span>
            </div>
          </template>
          <el-table :data="stagesMeta" stripe max-height="400">
            <el-table-column prop="label" label="阶段" width="100" />
            <el-table-column prop="description" label="说明" min-width="160" show-overflow-tooltip />
            <el-table-column prop="default_followup" label="默认跟进" width="120" />
            <el-table-column prop="owner_role" label="负责角色" width="100" align="center">
              <template #default="{ row }">
                <el-tag size="small" :type="getRoleTagType(row.owner_role)">
                  {{ getRoleLabel(row.owner_role) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="AI 接管" width="80" align="center">
              <template #default="{ row }">
                <el-tag v-if="row.allow_ai_handle" type="success" size="small">允许</el-tag>
                <el-tag v-else type="info" size="small">仅人工</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :span="12">
        <el-card class="detail-card">
          <template #header>
            <div class="card-header">
              <span>阶段客户分布</span>
            </div>
          </template>
          <div v-loading="loading">
            <el-table :data="overview?.stages || []" stripe max-height="400">
              <el-table-column prop="label" label="阶段" min-width="120" />
              <el-table-column prop="count" label="客户数" width="100" align="center" />
              <el-table-column label="占比" width="200">
                <template #default="{ row }">
                  <el-progress
                    :percentage="Math.round(row.rate)"
                    :stroke-width="14"
                    :color="rateColor(row.rate)"
                  />
                </template>
              </el-table-column>
              <el-table-column label="平均停留" width="120" align="center">
                <template #default="{ row }">
                  {{ row.avg_stay_hours.toFixed(0) }}h
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="events-card">
      <template #header>
        <div class="card-header">
          <span>
            <el-icon style="color: #10B981"><Bell /></el-icon>
            实时旅程事件流
          </span>
          <span class="card-tip">最近 20 条阶段迁移记录</span>
        </div>
      </template>
      <el-timeline>
        <el-timeline-item
          v-for="(event, idx) in recentEvents"
          :key="idx"
          :type="event.type === 'stage_transition' ? 'primary' : 'warning'"
          :timestamp="formatTime(event.timestamp)"
          placement="top"
        >
          <el-card shadow="never">
            <div class="event-item">
              <span class="event-customer">{{ event.customer_id }}</span>
              <span class="event-arrow">
                <el-tag size="small">{{ event.from_stage }}</el-tag>
                <el-icon><Right /></el-icon>
                <el-tag size="small" type="success">{{ event.to_stage }}</el-tag>
              </span>
              <span class="event-reason">{{ event.reason || '无原因' }}</span>
              <span class="event-source">来源: {{ getSourceLabel(event.source || 'manual') }}</span>
            </div>
          </el-card>
        </el-timeline-item>
        <el-empty v-if="recentEvents.length === 0" description="暂无事件" />
      </el-timeline>
    </el-card>

    <el-dialog v-model="stageDialog" :title="`${selectedStageLabel} · 客户明细`" width="600px">
      <el-table v-loading="stageLoading" :data="stageCustomers" stripe max-height="420" empty-text="该阶段暂无客户">
        <el-table-column prop="customer_id" label="客户ID" />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Refresh, User, CircleCheck, Sunny, Moon,
  DataLine, Bell, Right
} from '@element-plus/icons-vue'
import {
  getJourneyOverview, listJourneyStages, listByStage
} from '@/api/customerJourney.js'
import { getRoleLabel, getRoleTagType } from '@/constants/role';

const loading = ref(false)
const autoRefresh = ref(false)
const lastUpdate = ref('-')
const overview = ref(null)
const stagesMeta = ref([])
const recentEvents = ref([])
let refreshTimer = null

const stageList = computed(() => overview.value?.stages || [])

const wonCount = computed(() => {
  const won = stageList.value.find((s) => s.stage === 'won')
  return won?.count || 0
})

const interestedCount = computed(() => {
  const st = stageList.value.find((s) => s.stage === 'interested')
  return st?.count || 0
})

const sleepingCount = computed(() => {
  const st = stageList.value.find((s) => s.stage === 'sleeping')
  return st?.count || 0
})

const rateColor = (rate) => {
  if (rate >= 30) return '#10B981'
  if (rate >= 10) return '#F59E0B'
  return '#909399'
}

const stageColors = [
  '#909399', '#c0c4cc', '#4F46E5', '#10B981', '#F59E0B',
  '#EF4444', '#9b59b6', '#16a085', '#e74c3c', '#34495e'
];

const getFunnelStyle = (idx) => {
  const maxRate = 100
  const currentRate = stageList.value[idx]?.rate || 0
  const widthPct = Math.max(20, (currentRate / maxRate) * 100)
  return {
    width: `${widthPct}%`,
    background: stageColors[idx % stageColors.length]
  }
}

const formatTime = (val) => {
  if (!val) return '-'
  const d = new Date(val)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}

const stageDialog = ref(false)
const stageLoading = ref(false)
const stageCustomers = ref([])
const selectedStageLabel = ref('')
const selectStage = async (stage) => {
  selectedStageLabel.value = stage.label
  stageDialog.value = true
  stageLoading.value = true
  stageCustomers.value = []
  try {
    const res = await listByStage(stage.stage)
    const ids = (res && res.customer_ids) || []
    stageCustomers.value = ids.map((id) => ({ customer_id: id }))
  } catch (e) {
    ElMessage.error('加载阶段客户失败：' + (e && e.message ? e.message : ''))
  } finally {
    stageLoading.value = false
  }
}

const loadOverview = async () => {
  loading.value = true
  try {
    const [overviewRes, stagesRes] = await Promise.all([
      getJourneyOverview(),
      listJourneyStages()
    ])
    overview.value = overviewRes
    const stagesData = stagesRes || []
    stagesMeta.value = Array.isArray(stagesData) ? stagesData : stagesData.list || []
    lastUpdate.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } catch (e) {
    ElMessage.error('加载总览失败：' + (e?.message || ''))
  } finally {
    loading.value = false
  }
}

const toggleAutoRefresh = (val) => {
  if (val) {
    refreshTimer = setInterval(() => {
      loadOverview()
    }, 30000);
    ElMessage.success(i18n.global.t('已开启自动刷新（30秒）'))
  } else {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
    ElMessage.info(i18n.global.t('已关闭自动刷新'))
  }
}

onMounted(() => {
  loadOverview()
})

onUnmounted(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
})
</script>

<style scoped lang="scss">
.journey-dashboard { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
  .header-actions { display: flex; align-items: center; gap: 15px; }
}
.stat-row { margin-bottom: 20px; }
.stat-card {
  :deep(.el-card__body) {
    display: flex;
    align-items: center;
    padding: 20px;
  }
  .stat-icon {
    width: 50px;
    height: 50px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-right: 15px;
    color: #fff;
    font-size: 26px;
  }
  &.stat-blue .stat-icon { background: #4F46E5; }
  &.stat-green .stat-icon { background: #10B981; }
  &.stat-orange .stat-icon { background: #F59E0B; }
  &.stat-red .stat-icon { background: #EF4444; }
  .stat-content {
    flex: 1;
    .stat-label { color: #909399; font-size: 13px; margin-bottom: 6px; }
    .stat-value { font-size: 28px; font-weight: bold; line-height: 1.2; }
  }
}
.funnel-card { margin-bottom: 20px; }
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  .card-tip { color: #909399; font-size: 12px; }
}
.funnel-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 0;
  min-height: 400px;
  .funnel-stage {
    position: relative;
    margin-bottom: 8px;
    transition: all 0.3s;
    cursor: pointer;
    border-radius: 6px;
    text-align: center;
    color: #fff;
    padding: 15px 20px;
    min-width: 200px;
    &:hover {
      transform: scale(1.02);
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    }
    .funnel-stage-label { font-size: 16px; font-weight: 600; margin-bottom: 4px; }
    .funnel-stage-count { font-size: 22px; font-weight: bold; margin-bottom: 4px; }
    .funnel-stage-rate { font-size: 12px; opacity: 0.9; }
    .funnel-arrow {
      position: absolute;
      bottom: -16px;
      left: 50%;
      transform: translateX(-50%);
      color: #909399;
      font-size: 18px;
      line-height: 1;
    }
  }
}
.detail-row { margin-bottom: 20px; }
.detail-card { height: 100%; }
.events-card { margin-bottom: 20px; }
.event-item {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  .event-customer { font-weight: 600; }
  .event-arrow { display: flex; align-items: center; gap: 4px; color: #909399; }
  .event-reason { color: #606266; font-size: 13px; }
  .event-source { color: #909399; font-size: 12px; }
}
</style>
