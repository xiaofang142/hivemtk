<template>
  <div class="conversion-funnel">
    <el-card class="page-card">
      <div class="page-header">
        <div>
          <h2 class="page-title">转化漏斗</h2>
          <p class="page-sub">访问 → 线索 → 意向 → 会话 全链路转化分析</p>
        </div>
        <el-button type="primary" @click="loadAll">刷新</el-button>
      </div>

      <el-row :gutter="16" class="summary-row">
        <el-col :span="6">
          <el-statistic title="总进入量" :value="summary.totalEnter" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="转化量(会话)" :value="summary.totalConvert" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="端到端转化率" :value="summary.overallRate" suffix="%" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="平均流失率" :value="summary.avgLossRate" suffix="%" />
        </el-col>
      </el-row>

      <el-row :gutter="16" class="chart-row">
        <el-col :span="14">
          <el-card shadow="never" header="漏斗图">
            <div ref="funnelChart" class="chart-box"></div>
          </el-card>
        </el-col>
        <el-col :span="10">
          <el-card shadow="never" header="阶段明细">
            <el-table :data="stageTable" stripe>
              <el-table-column prop="name" label="阶段" />
              <el-table-column prop="count" label="数量" />
              <el-table-column label="阶段转化率">
                <template #default="{ row }">{{ (row.rate || 0).toFixed(1) }}%</template>
              </el-table-column>
              <el-table-column label="流失率">
                <template #default="{ row }">{{ (row.dropRate || 0).toFixed(1) }}%</template>
              </el-table-column>
            </el-table>
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never" header="阶段流失分析" class="loss-card">
        <el-form inline>
          <el-form-item label="选择阶段">
            <el-select v-model="selectedStage" placeholder="选择阶段" @change="loadLoss">
              <el-option v-for="s in stageTable" :key="s.stage" :label="s.name" :value="s.stage" />
            </el-select>
          </el-form-item>
          <el-button type="primary" @click="loadLoss">查询</el-button>
        </el-form>
        <el-descriptions v-if="loss" :column="3" border>
          <el-descriptions-item label="阶段">{{ loss.name }}</el-descriptions-item>
          <el-descriptions-item label="数量">{{ loss.count }}</el-descriptions-item>
          <el-descriptions-item label="阶段转化率">{{ (loss.rate || 0).toFixed(1) }}%</el-descriptions-item>
          <el-descriptions-item label="平均停留(秒)">{{ loss.avgDuration.toFixed(1) }}</el-descriptions-item>
        </el-descriptions>
        <el-table v-if="loss && loss.topSources.length" :data="loss.topSources" stripe class="src-table">
          <el-table-column label="来源(账号)">
            <template #default="{ row }">{{ getChannelLabel(row.source) }}</template>
          </el-table-column>
          <el-table-column prop="count" label="数量" />
        </el-table>
      </el-card>
    </el-card>
  </div>
</template>

<script>
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import ConversionFunnelApi from '@/api/conversionFunnel'
import { getChannelLabel } from '@/constants/channel'

export default {
  name: 'ConversionFunnelList',
  data() {
    return {
      report: null,
      stageTable: [],
      summary: { totalEnter: 0, totalConvert: 0, overallRate: 0, avgLossRate: 0 },
      selectedStage: 'clue',
      loss: null,
      funnelChart: null,
      chartInst: null
    }
  },
  mounted() {
    this.chartInst = safeInit(this.$refs.funnelChart)
    this.loadAll()
  },
  beforeUnmount() {
    this._destroyed = true
    if (this.chartInst) {
      this.chartInst.dispose()
      this.chartInst = null
    }
  },
  methods: {
    getChannelLabel,
    async loadAll() {
      const res = await ConversionFunnelApi.getFunnel({})
      if (this._destroyed) return
      this.report = res || {}
      const stages = Array.isArray(this.report.stages) ? this.report.stages : []
      this.stageTable = stages.map((s) => ({
        stage: s.stage,
        name: s.name,
        count: s.count,
        rate: s.rate || 0,
        dropRate: s.drop_rate || 0
      }))
      const firstStage = stages[0]
      const lastStage = stages[stages.length - 1]
      const firstCount = firstStage ? firstStage.count : 0
      this.summary = {
        totalEnter: firstCount,
        totalConvert: lastStage ? lastStage.count : 0,
        overallRate: firstCount && lastStage ? (lastStage.count / firstCount) * 100 : 0,
        avgLossRate: stages.length ? stages.reduce((a, s) => a + (s.drop_rate || 0), 0) / stages.length : 0
      }
      this.renderFunnel()
      this.loadLoss()
    },
    async loadLoss() {
      if (!this.selectedStage) return
      const res = await ConversionFunnelApi.getStageDetails(this.selectedStage, {})
      if (this._destroyed) return
      const d = res || {}
      this.loss = {
        name: d.name || this.selectedStage,
        count: d.count || 0,
        rate: d.rate || 0,
        avgDuration: d.avg_duration_seconds || 0,
        topSources: Array.isArray(d.top_sources) ? d.top_sources : []
      }
    },
    renderFunnel() {
      if (!this.chartInst) return
      this.chartInst.setOption({
        tooltip: { trigger: 'item', formatter: '{b}: {c}' },
        series: [
          {
            type: 'funnel',
            left: '10%',
            width: '80%',
            label: { show: true, position: 'inside', formatter: '{b}: {c}' },
            data: this.stageTable.map((s) => ({ name: s.name, value: s.count }))
          }
        ]
      })
    }
  }
}
</script>

<style scoped>
.page-card { padding: 8px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-title { margin: 0; font-size: 20px; }
.page-sub { margin: 4px 0 0; color: #909399; font-size: 13px; }
.summary-row { margin-bottom: 16px; }
.chart-row { margin-bottom: 16px; }
.chart-box { height: 320px; }
.loss-card { margin-top: 8px; }
.src-table { margin-top: 12px; }
</style>
