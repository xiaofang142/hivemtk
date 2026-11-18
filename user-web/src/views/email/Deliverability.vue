<template>
  <div class="deliverability">
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            送达率
          </div>
          <div class="stat-value">
            {{ stats.deliveryRate?.toFixed(1) }}%
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">
            打开率
          </div>
          <div class="stat-value">
            {{ stats.openRate?.toFixed(1) }}%
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card warning">
          <div class="stat-label">
            软退信
          </div>
          <div class="stat-value">
            {{ stats.softBounce }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card danger">
          <div class="stat-label">
            硬退信
          </div>
          <div class="stat-value">
            {{ stats.hardBounce }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>域名信誉</span>
          <el-button
            size="small"
            @click="refresh"
          >
            刷新
          </el-button>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="domainReputation"
      >
        <el-table-column
          prop="domain"
          label="域名"
        />
        <el-table-column
          prop="reputation"
          label="信誉"
          width="120"
        >
          <template #default="{ row }">
            <el-rate
              v-model="row.reputation"
              disabled
              :max="5"
              show-score
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="sentLast24h"
          label="24h 发送"
          width="120"
        />
        <el-table-column
          prop="delivered"
          label="已送达"
          width="100"
        />
        <el-table-column
          prop="bounced"
          label="退信"
          width="80"
        />
        <el-table-column
          prop="complained"
          label="投诉"
          width="80"
        />
        <el-table-column
          prop="blacklisted"
          label="黑名单"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              v-if="row.blacklisted"
              type="danger"
              size="small"
            >
              已列入
            </el-tag>
            <el-tag
              v-else
              type="success"
              size="small"
            >
              正常
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="120"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="warning"
              :disabled="row.blacklisted"
              @click="suspendDomain(row)"
            >
              暂停使用
            </el-button>
            <el-button
              link
              type="primary"
              @click="viewDetails(row)"
            >
              详情
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card class="bounce-card">
      <template #header>
        <span>退信分类（按 ISP）</span>
      </template>
      <div
        ref="bounceChartRef"
        class="chart"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import * as _echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import { http } from '@/utils/request'

const stats = ref({ deliveryRate: 0, openRate: 0, softBounce: 0, hardBounce: 0 })
const domainReputation = ref([])
const loading = ref(false)
const bounceChartRef = ref()

async function load() {
  loading.value = true
  try {
    const [s, domains, bounces] = await Promise.all([
      http.get('/api/email/deliverability'),
      http.get('/api/email/domain-reputation'),
      http.get('/api/email/bounces/breakdown')
    ])
    stats.value = s || stats.value
    domainReputation.value = domains || []
    renderBounceChart(bounces || [])
  } finally {
    loading.value = false
  }
}

function refresh() { load() }

function renderBounceChart(bounces) {
  if (!bounceChartRef.value) return
  const chart = safeInit(bounceChartRef.value)
  chart.setOption({
    tooltip: { trigger: 'item' },
    series: [{
      type: 'pie',
      radius: ['40%', '70%'],
      data: bounces.map((b) => ({ name: b.isp, value: b.count }))
    }]
  })
}

async function suspendDomain(row) {
  try {
    await ElMessageBox.confirm(`确认暂停域名「${row.domain}」？`, '暂停确认', { type: 'warning' })
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    throw e
  }
  await http.post(`/api/email/domains/${row.id}/suspend`, {})
  ElMessage.success('已暂停')
  await load()
}

function viewDetails(row) {
  window.open(`/email/domain-detail?id=${row.id}`, '_blank')
}

onMounted(load)
</script>

<style scoped>
.deliverability { padding: 16px; }
.stats-row { margin-bottom: 16px; }
.stat-card { text-align: center; padding: 8px; }
.stat-label { color: #64748B; font-size: 12px; }
.stat-value { font-size: 32px; font-weight: 700; margin: 8px 0; }
.stat-card.success .stat-value { color: #10B981; }
.stat-card.warning .stat-value { color: #F59E0B; }
.stat-card.danger .stat-value { color: #EF4444; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.bounce-card { margin-top: 16px; }
.chart { width: 100%; height: 320px; }
</style>
