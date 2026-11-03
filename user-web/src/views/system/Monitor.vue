<template>
  <div class="monitor-page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('服务器监控') }}</span>
          <el-button
            type="primary"
            @click="refresh"
          >
            {{ $t('刷新') }}
          </el-button>
        </div>
      </template>

      <el-row :gutter="16">
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              CPU使用率
            </div>
            <div class="metric-value">
              {{ formatFloat(metrics.cpu_usage) }}%
            </div>
          </el-card>
        </el-col>
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              内存使用率
            </div>
            <div class="metric-value">
              {{ formatFloat(metrics.memory_usage) }}%
            </div>
          </el-card>
        </el-col>
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              磁盘使用率
            </div>
            <div class="metric-value">
              {{ formatFloat(metrics.disk_usage) }}%
            </div>
          </el-card>
        </el-col>
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              网络速度
            </div>
            <div class="metric-value">
              {{ formatFloat(metrics.network_speed) }} Mbps
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-row
        :gutter="16"
        style="margin-top: 16px"
      >
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              运行时间
            </div>
            <div class="metric-value">
              {{ formatUptime(metrics.uptime) }}
            </div>
          </el-card>
        </el-col>
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              服务器时间
            </div>
            <div class="metric-value">
              {{ fmtTime(metrics.server_time) }}
            </div>
          </el-card>
        </el-col>
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              主机名
            </div>
            <div class="metric-value">
              {{ metrics.hostname }}
            </div>
          </el-card>
        </el-col>
        <el-col
          :md="6"
          :sm="12"
          :xs="24"
        >
          <el-card class="metric-card">
            <div class="metric-title">
              Go版本
            </div>
            <div class="metric-value">
              {{ metrics.go_version }}
            </div>
          </el-card>
        </el-col>
      </el-row>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { StatsApi } from '@/api/stats'
import { fmtTime } from '@/utils/time'

const metrics = ref({
  cpu_usage: 0,
  memory_usage: 0,
  disk_usage: 0,
  network_speed: 0,
  uptime: 0,
  server_time: '',
  hostname: '',
  go_version: ''
})

const load = async () => {
  try {
    const res = await StatsApi.getSystemInfo()
    metrics.value = res || metrics.value
  } catch (e) {
    ElMessage.error(i18n.global.t('获取系统监控数据失败'))
  }
}

const refresh = () => load()

const formatFloat = (val) => {
  const n = Number(val)
  if (Number.isFinite(n)) return n.toFixed(2)
  return '0.00'
}

const formatUptime = (seconds) => {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${d}天 ${h}小时 ${m}分钟`
}

onMounted(load)
</script>

<style scoped>
.monitor-page { padding: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.metric-card { text-align: center; }
.metric-title { color: #909399; font-size: 14px; }
.metric-value { font-size: 22px; font-weight: 600; margin-top: 6px; }
</style>
