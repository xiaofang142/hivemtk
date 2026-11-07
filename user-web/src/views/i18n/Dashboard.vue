<template>
  <div class="i18n-dashboard-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="header-content">
        <div>
          <h2>{{ $t('多语言监控') }}</h2>
          <p class="subtitle">
            翻译服务运行态总览 · 语言分布 / 缓存命中 / 术语覆盖 / 质量与延迟
          </p>
        </div>
        <div class="header-actions">
          <el-select
            v-model="days"
            style="width: 130px"
            @change="loadAll"
          >
            <el-option
              :label="$t('近 7 天')"
              :value="7"
            />
            <el-option
              :label="$t('近 14 天')"
              :value="14"
            />
            <el-option
              :label="$t('近 30 天')"
              :value="30"
            />
          </el-select>
          <el-button
            :loading="loading"
            @click="loadAll"
          >
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-row
      :gutter="16"
      class="kpi-row"
    >
      <el-col
        :xs="12"
        :sm="12"
        :md="6"
      >
        <el-card
          shadow="never"
          class="kpi-card kpi-blue"
        >
          <div class="kpi-icon">
            <el-icon><ChatLineRound /></el-icon>
          </div>
          <div class="kpi-body">
            <div class="kpi-label">
              {{ $t('总调用量') }}
            </div>
            <div class="kpi-value">
              {{ formatNumber(overview.total_calls) }}
            </div>
            <div class="kpi-sub">
              {{ $t('累计翻译请求数') }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="12"
        :md="6"
      >
        <el-card
          shadow="never"
          class="kpi-card kpi-purple"
        >
          <div class="kpi-icon">
            <el-icon><Connection /></el-icon>
          </div>
          <div class="kpi-body">
            <div class="kpi-label">
              {{ $t('跨语言调用占比') }}
            </div>
            <div class="kpi-value">
              {{ crossLingualRatio }}%
            </div>
            <div class="kpi-sub">
              {{ formatNumber(overview.cross_lingual_calls) }} / {{ formatNumber(overview.total_calls) }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="12"
        :md="6"
      >
        <el-card
          shadow="never"
          class="kpi-card kpi-green"
        >
          <div class="kpi-icon">
            <el-icon><Files /></el-icon>
          </div>
          <div class="kpi-body">
            <div class="kpi-label">
              {{ $t('缓存命中率') }}
            </div>
            <div class="kpi-value">
              {{ formatPercent(overview.cache_hit_rate) }}%
            </div>
            <div class="kpi-sub">
              {{ $t('回退率') }} {{ formatPercent(overview.fallback_rate) }}%
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col
        :xs="12"
        :sm="12"
        :md="6"
      >
        <el-card
          shadow="never"
          class="kpi-card kpi-orange"
        >
          <div class="kpi-icon">
            <el-icon><TrendCharts /></el-icon>
          </div>
          <div class="kpi-body">
            <div class="kpi-label">
              {{ $t('平均质量评分') }}
            </div>
            <div class="kpi-value">
              {{ formatScore(overview.avg_quality) }}
            </div>
            <div class="kpi-sub">
              {{ $t('满分 1.0') }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-row
      :gutter="16"
      class="section-row"
    >
      <el-col
        :xs="24"
        :md="14"
      >
        <el-card
          shadow="never"
          class="section-card"
        >
          <template #header>
            <div class="card-title">
              <el-icon><DataLine /></el-icon>
              <span>{{ $t('语言分布（内部 → 目标）') }}</span>
            </div>
          </template>
          <div
            ref="langDistChart"
            v-loading="loading"
            class="chart-box"
          />
          <el-empty
            v-if="!langDist.length && !loading"
            :description="$t('暂无语言分布数据')"
            :image-size="60"
          />
        </el-card>
      </el-col>
      <el-col
        :xs="24"
        :md="10"
      >
        <el-card
          shadow="never"
          class="section-card"
        >
          <template #header>
            <div class="card-title">
              <el-icon><Files /></el-icon>
              <span>{{ $t('缓存命中率') }}</span>
            </div>
          </template>
          <div
            v-loading="loading"
            class="cache-block"
          >
            <div class="cache-stats">
              <div class="cache-item">
                <span class="cache-label">{{ $t('命中') }}</span>
                <span class="cache-value cache-hit">{{ formatNumber(cacheStats.hit) }}</span>
              </div>
              <div class="cache-item">
                <span class="cache-label">{{ $t('未命中') }}</span>
                <span class="cache-value cache-miss">{{ formatNumber(cacheStats.miss) }}</span>
              </div>
              <div class="cache-item">
                <span class="cache-label">{{ $t('命中率') }}</span>
                <span class="cache-value">{{ formatPercent(cacheStats.hit_rate) }}%</span>
              </div>
            </div>
            <el-progress
              :percentage="Number(formatPercent(cacheStats.hit_rate))"
              :stroke-width="18"
              :color="cacheHitColor"
              :format="(p) => `${p}%`"
            />
            <div class="cache-tip">
              {{ $t('缓存命中率越高，回源翻译开销越低') }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-row
      :gutter="16"
      class="section-row"
    >
      <el-col
        :xs="24"
        :md="14"
      >
        <el-card
          shadow="never"
          class="section-card"
        >
          <template #header>
            <div class="card-title">
              <el-icon><TrendCharts /></el-icon>
              <span>{{ $t('质量评分趋势') }}</span>
            </div>
          </template>
          <div
            ref="qualityChart"
            v-loading="loading"
            class="chart-box"
          />
          <el-empty
            v-if="!qualityTrend.length && !loading"
            :description="$t('暂无质量数据')"
            :image-size="60"
          />
        </el-card>
      </el-col>
      <el-col
        :xs="24"
        :md="10"
      >
        <el-card
          shadow="never"
          class="section-card"
        >
          <template #header>
            <div class="card-title">
              <el-icon><Collection /></el-icon>
              <span>{{ $t('术语覆盖率') }}</span>
            </div>
          </template>
          <el-table
            v-loading="loading"
            :data="glossaryCoverage"
            stripe
            size="small"
            border
          >
            <el-table-column
              label="目标语言"
              min-width="120"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  type="success"
                >
                  {{ getLanguageLabel(row.target_lang) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="term_count"
              label="术语总数"
              width="100"
              align="center"
            />
            <el-table-column
              prop="active_count"
              label="启用数"
              width="90"
              align="center"
            />
            <el-table-column
              label="覆盖率"
              min-width="140"
            >
              <template #default="{ row }">
                <el-progress
                  :percentage="glossaryCovPercent(row)"
                  :stroke-width="12"
                  :color="covColor(glossaryCovPercent(row))"
                  :show-text="true"
                />
              </template>
            </el-table-column>
            <template #empty>
              <el-empty
                :description="$t('暂无术语覆盖数据')"
                :image-size="50"
              />
            </template>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card
      shadow="never"
      class="section-card"
    >
      <template #header>
        <div class="card-title">
          <el-icon><Timer /></el-icon>
          <span>{{ $t('延迟统计（按目标语言，单位 ms）') }}</span>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="latencyStats"
        stripe
        border
      >
        <el-table-column
          label="目标语言"
          min-width="140"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ getLanguageLabel(row.target_lang) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="P50"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <span class="latency-num">{{ formatMs(row.p50) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="P95"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <span
              class="latency-num"
              :class="latencyClass(row.p95)"
            >{{ formatMs(row.p95) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          label="P99"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <span
              class="latency-num"
              :class="latencyClass(row.p99)"
            >{{ formatMs(row.p99) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="count"
          label="样本数"
          width="120"
          align="center"
        />
        <el-table-column
          label="P95 分布"
          min-width="200"
        >
          <template #default="{ row }">
            <el-progress
              :percentage="latencyBarPercent(row.p95)"
              :stroke-width="10"
              :color="latencyColor(row.p95)"
              :show-text="false"
            />
          </template>
        </el-table-column>
        <template #empty>
          <el-empty
            :description="$t('暂无延迟数据')"
            :image-size="60"
          />
        </template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import * as echarts from 'echarts'
import { safeInit } from '@/utils/echarts'
import {
  Refresh, ChatLineRound, Connection, Files, TrendCharts,
  DataLine, Collection, Timer
} from '@element-plus/icons-vue'
import {
  getI18nStats,
  getLangDistribution,
  getCacheHitRate,
  getGlossaryCoverage,
  getQualityTrend,
  getLatencyStats
} from '@/api/i18nStats.js'
import { getLanguageLabel } from '@/constants/languages'

const t = i18n.global.t

const loading = ref(false);
const days = ref(7)
const overview = ref({
  total_calls: 0,
  cross_lingual_calls: 0,
  cache_hit_rate: 0,
  fallback_rate: 0,
  avg_quality: 0
})
const langDist = ref([])
const cacheStats = ref({ hit: 0, miss: 0, hit_rate: 0 })
const glossaryCoverage = ref([])
const qualityTrend = ref([])
const latencyStats = ref([])

const langDistChart = ref(null);
const qualityChart = ref(null)
let langDistInst = null
let qualityInst = null

const crossLingualRatio = computed(() => {
  const total = Number(overview.value.total_calls || 0)
  const cross = Number(overview.value.cross_lingual_calls || 0)
  if (!total) return '0.0'
  return ((cross / total) * 100).toFixed(1)
});

const cacheHitColor = computed(() => {
  const p = formatPercent(cacheStats.value.hit_rate)
  if (p >= 80) return '#67C23A'
  if (p >= 50) return '#E6A23C'
  return '#F56C6C'
})

const formatNumber = (n) => {
  const v = Number(n || 0)
  if (!Number.isFinite(v)) return '0'
  return v.toLocaleString('en-US')
};

const formatPercent = (n) => {
  const v = Number(n || 0)
  if (!Number.isFinite(v)) return '0.0'
  if (v > 0 && v <= 1)
    return (v * 100).toFixed(1);
  return v.toFixed(1)
}

const formatScore = (n) => {
  const v = Number(n || 0)
  if (!Number.isFinite(v)) return '0.00'
  return v.toFixed(2)
}

const formatMs = (n) => {
  const v = Number(n || 0)
  if (!Number.isFinite(v)) return '-'
  return v.toFixed(0)
}

const glossaryCovPercent = (row) => {
  const total = Number(row?.term_count || 0)
  const active = Number(row?.active_count || 0)
  if (!total) return 0
  const p = (active / total) * 100
  // 保留 1 位小数：直接返回原始浮点会让进度条文案出现 66.66666666666666%
  return Math.round(Math.min(100, Math.max(0, p)) * 10) / 10
};

const covColor = (p) => {
  if (p >= 80) return '#67C23A'
  if (p >= 50) return '#E6A23C'
  return '#F56C6C'
}

const latencyClass = (ms) => {
  const v = Number(ms || 0)
  if (v >= 1000) return 'latency-danger'
  if (v >= 500) return 'latency-warning'
  return 'latency-ok'
};

const latencyColor = (ms) => latencyToColor(ms)

const latencyToColor = (ms) => {
  const v = Number(ms || 0)
  if (v >= 1000) return '#F56C6C'
  if (v >= 500) return '#E6A23C'
  return '#67C23A'
}

const latencyBarPercent = (ms) => {
  const v = Number(ms || 0)
  return Math.min(100, Math.max(0, (v / 2000) * 100));
}

const loadAll = async () => {
  loading.value = true
  try {
    const [ov, ld, ch, gc, qt, ls] = await Promise.all([
      getI18nStats().catch((e) => { console.warn('i18n stats error', e); return null }),
      getLangDistribution(days.value).catch((e) => { console.warn('lang-dist error', e); return [] }),
      getCacheHitRate(days.value).catch((e) => { console.warn('cache error', e); return null }),
      getGlossaryCoverage().catch((e) => { console.warn('glossary error', e); return [] }),
      getQualityTrend(30).catch((e) => { console.warn('quality error', e); return [] }),
      getLatencyStats(days.value).catch((e) => { console.warn('latency error', e); return [] })
    ])
    overview.value = ov || overview.value
    langDist.value = Array.isArray(ld) ? ld : (ld?.list || [])
    cacheStats.value = ch || cacheStats.value
    glossaryCoverage.value = Array.isArray(gc) ? gc : (gc?.list || [])
    qualityTrend.value = Array.isArray(qt) ? qt : (qt?.list || [])
    latencyStats.value = Array.isArray(ls) ? ls : (ls?.list || [])

    await nextTick()
    renderLangDist()
    renderQuality()
  } catch (e) {
    ElMessage.error(t('加载监控数据失败') + '：' + (e.message || '未知错误'))
  } finally {
    loading.value = false
  }
};

const renderLangDist = () => {
  if (!langDistChart.value) return
  if (!langDistInst) {
    langDistInst = safeInit(langDistChart.value)
  }
  const data = langDist.value.map((row) => ({
    name: `${getLanguageLabel(row.internal_lang)} → ${getLanguageLabel(row.target_lang)}`,
    value: Number(row.count || 0),
    cross: Number(row.cross_lingual_count || 0)
  })).filter((d) => d.value > 0).sort((a, b) => b.value - a.value).slice(0, 15)

  if (!data.length) {
    langDistInst.clear()
    return
  }

  langDistInst.setOption({
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: (params) => {
        const p = params[0]
        const item = data[p.dataIndex]
        if (!item) return p.name
        return `${item.name}<br/>${t('调用数')}: ${item.value}<br/>${t('跨语言')}: ${item.cross}`
      }
    },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'value', name: t('调用数') },
    yAxis: { type: 'category', data: data.map((d) => d.name), inverse: true },
    series: [{
      name: t('调用数'),
      type: 'bar',
      data: data.map((d) => d.value),
      itemStyle: {
        color: new echarts.graphic.LinearGradient(0, 0, 1, 0, [
          { offset: 0, color: '#6366F1' },
          { offset: 1, color: '#4F46E5' }
        ]),
        borderRadius: [0, 4, 4, 0]
      },
      label: { show: true, position: 'right', formatter: '{c}' }
    }]
  }, true)
};

const renderQuality = () => {
  if (!qualityChart.value) return
  if (!qualityInst) {
    qualityInst = safeInit(qualityChart.value)
  }
  const data = qualityTrend.value.slice().sort((a, b) => {
    const da = new Date(a.date).getTime() || 0
    const db = new Date(b.date).getTime() || 0
    return da - db
  })

  if (!data.length) {
    qualityInst.clear()
    return
  }

  qualityInst.setOption({
    tooltip: {
      trigger: 'axis',
      formatter: (params) => {
        const p = params[0]
        const item = data[p.dataIndex]
        if (!item) return p.name
        return `${item.date}<br/>${t('平均评分')}: ${Number(item.avg_score || 0).toFixed(2)}<br/>${t('样本数')}: ${item.total_count || 0}`
      }
    },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: {
      type: 'category',
      data: data.map((d) => d.date),
      boundaryGap: false
    },
    yAxis: {
      type: 'value',
      name: t('评分'),
      min: 0,
      max: 1,
      interval: 0.2
    },
    series: [{
      name: t('平均评分'),
      type: 'line',
      smooth: true,
      data: data.map((d) => Number(d.avg_score || 0)),
      itemStyle: { color: '#4F46E5' },
      lineStyle: { width: 3 },
      areaStyle: {
        color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
          { offset: 0, color: 'rgba(79, 70, 229, 0.25)' },
          { offset: 1, color: 'rgba(79, 70, 229, 0.02)' }
        ])
      },
      symbol: 'circle',
      symbolSize: 6
    }]
  }, true)
}

const onResize = () => {
  langDistInst && langDistInst.resize()
  qualityInst && qualityInst.resize()
};

onMounted(() => {
  loadAll()
  window.addEventListener('resize', onResize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  if (langDistInst) { langDistInst.dispose(); langDistInst = null }
  if (qualityInst) { qualityInst.dispose(); qualityInst = null }
})
</script>

<style scoped lang="scss">
.i18n-dashboard-page { padding: 20px; }

.header-card {
  margin-bottom: 16px;
  :deep(.el-card__body) { padding: 16px 20px; }
  .header-content {
    display: flex;
    justify-content: space-between;
    align-items: center;
    h2 { margin: 0 0 6px 0; font-size: 20px; }
    .subtitle { color: #909399; margin: 0; font-size: 13px; }
    .header-actions { display: flex; gap: 8px; align-items: center; }
  }
}

/* ===== KPI 卡片 ===== */
.kpi-row { margin-bottom: 16px; }
.kpi-card {
  :deep(.el-card__body) {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 18px 20px;
  }
  .kpi-icon {
    width: 48px;
    height: 48px;
    border-radius: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #fff;
    font-size: 24px;
    flex-shrink: 0;
  }
  .kpi-body { flex: 1; min-width: 0; }
  .kpi-label {
    font-size: 13px;
    color: #64748B;
    margin-bottom: 4px;
  }
  .kpi-value {
    font-size: 26px;
    font-weight: 700;
    color: #0F172A;
    line-height: 1.1;
  }
  .kpi-sub {
    font-size: 12px;
    color: #94A3B8;
    margin-top: 4px;
  }
}
.kpi-blue .kpi-icon { background: linear-gradient(135deg, #6366F1, #4F46E5); }
.kpi-purple .kpi-icon { background: linear-gradient(135deg, #A855F7, #7C3AED); }
.kpi-green .kpi-icon { background: linear-gradient(135deg, #10B981, #059669); }
.kpi-orange .kpi-icon { background: linear-gradient(135deg, #F59E0B, #D97706); }

/* ===== 区块卡片 ===== */
.section-row { margin-bottom: 16px; }
.section-card {
  margin-bottom: 16px;
  .card-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    color: #303133;
    .el-icon { color: #4F46E5; }
  }
}

.chart-box {
  width: 100%;
  height: 320px;
}

/* ===== 缓存区块 ===== */
.cache-block { padding: 8px 4px; }
.cache-stats {
  display: flex;
  justify-content: space-around;
  margin-bottom: 18px;
  .cache-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
  }
  .cache-label { font-size: 13px; color: #64748B; }
  .cache-value { font-size: 22px; font-weight: 700; color: #0F172A; }
  .cache-hit { color: #67C23A; }
  .cache-miss { color: #F56C6C; }
}
.cache-tip {
  margin-top: 12px;
  font-size: 12px;
  color: #94A3B8;
  text-align: center;
}

/* ===== 延迟表格 ===== */
.latency-num {
  font-weight: 600;
  font-family: 'Menlo', 'Monaco', monospace;
}
.latency-ok { color: #67C23A; }
.latency-warning { color: #E6A23C; }
.latency-danger { color: #F56C6C; }
</style>
