<template>
  <div class="ai-productivity">
    <el-card class="page-card">
      <div class="page-header">
        <div>
          <h2 class="page-title">AI 产能分析</h2>
          <p class="page-sub">AI 回复占比、响应时长、转化率与 Token 成本概览</p>
        </div>
        <el-button type="primary" @click="loadAll">刷新</el-button>
      </div>

      <el-row :gutter="16" class="kpi-row">
        <el-col :span="6">
          <el-statistic title="总会话数" :value="rep.total_conversations" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="AI 回复占比" :value="rep.ai_ratio" suffix="%" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="平均响应时长(秒)" :value="Number(Number(rep.avg_response_time || 0).toFixed(1))" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="转化率" :value="rep.conversion_rate" suffix="%" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="AI 回复数" :value="rep.ai_replies" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="人工回复数" :value="rep.human_replies" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="转化数(成单)" :value="rep.total_conversions" />
        </el-col>
        <el-col :span="6">
          <el-statistic title="LLM Token" :value="rep.llm_tokens" />
        </el-col>
      </el-row>

      <el-card shadow="never" header="近 30 天趋势" class="trend-card">
        <div ref="trendChart" class="chart-box"></div>
      </el-card>
    </el-card>
  </div>
</template>

<script>
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import AIProductivityApi from '@/api/aiProductivity'

export default {
  name: 'AIProductivityList',
  data() {
    return {
      rep: {},
      trend: [],
      trendChart: null,
      chartInst: null
    }
  },
  mounted() {
    this.chartInst = safeInit(this.$refs.trendChart)
    this.loadAll()
  },
  beforeUnmount() {
    if (this.chartInst) this.chartInst.dispose()
  },
  methods: {
    async loadAll() {
      const [rep, trendRes] = await Promise.all([
        AIProductivityApi.getReport({}),
        AIProductivityApi.getTrend({ days: 30 })
      ])
      this.rep = rep || {}
      const t = trendRes && Array.isArray(trendRes.trend) ? trendRes.trend : (Array.isArray(trendRes) ? trendRes : [])
      this.trend = t
      this.renderTrend()
    },
    renderTrend() {
      if (!this.chartInst) return
      const dates = this.trend.map((d) => d.date)
      const conv = this.trend.map((d) => d.conversations)
      const ai = this.trend.map((d) => d.ai_replies)
      const orders = this.trend.map((d) => d.conversions)
      this.chartInst.setOption({
        tooltip: { trigger: 'axis' },
        legend: { data: ['会话数', 'AI 回复数', '转化数'] },
        xAxis: { type: 'category', data: dates },
        yAxis: { type: 'value' },
        series: [
          { name: '会话数', type: 'line', data: conv },
          { name: 'AI 回复数', type: 'line', data: ai },
          { name: '转化数', type: 'bar', data: orders }
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
.kpi-row { margin-bottom: 16px; }
.trend-card { margin-top: 8px; }
.chart-box { height: 340px; }
</style>
