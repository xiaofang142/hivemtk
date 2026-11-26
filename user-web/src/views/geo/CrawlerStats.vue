<template>
  <div class="geo-page p-4">
    <el-alert
      :title="t('crawlerSource.title')"
      type="info"
      :closable="false"
      show-icon
      class="mb-4"
    >
      <p>{{ t('crawlerSource.sourceDescription') }}</p>
      <p>{{ t('crawlerSource.historyNote') }}</p>
      <p>{{ t('crawlerSource.allocationNote') }}</p>
      <p v-if="sourceMeta">
        {{ t('crawlerSource.configuration', { uas: sourceMeta.configured_ua_count, groups: sourceMeta.configured_engine_groups }) }}
        {{ t('crawlerSource.window', { days: sourceMeta.window_days }) }}
      </p>
    </el-alert>
    <el-alert
      v-if="unattributed"
      :title="t('crawlerSource.unattributed')"
      type="warning"
      :closable="false"
      class="mb-4"
    />
    <el-alert
      v-if="loadFailed"
      :title="t('crawlerSource.loadFailed')"
      type="error"
      :closable="false"
      class="mb-4"
    />
    <el-row
      :gutter="12"
      class="mb-4"
    >
      <el-col :span="6">
        <el-card
          shadow="hover"
          class="coverage-card"
        >
          <div class="text-center py-2">
            <div class="text-xs text-gray-500 mb-1">
              {{ t('crawlerSource.selfShare') }}
            </div>
            <div
              class="text-3xl font-bold"
              :style="{ color: coverageColor }"
            >
              {{ (coverageScore || 0).toFixed(1) }}%
            </div>
            <el-progress
              :percentage="Math.round(coverageScore || 0)"
              :color="coverageColor"
              :stroke-width="8"
              class="mt-2"
            />
          </div>
        </el-card>
      </el-col>
      <el-col :span="3">
        <el-card shadow="hover">
          <el-statistic
            :title="t('crawlerSource.last24Hours')"
            :value="s.last_24h_probes || 0"
          />
        </el-card>
      </el-col>
      <el-col :span="3">
        <el-card shadow="hover">
          <el-statistic
            :title="t('crawlerSource.selfProbes')"
            :value="s.self_site_probes || 0"
          />
        </el-card>
      </el-col>
      <el-col :span="3">
        <el-card shadow="hover">
          <el-statistic
            :title="t('crawlerSource.competitorProbes')"
            :value="s.competitor_probes || 0"
          />
        </el-card>
      </el-col>
      <el-col :span="3">
        <el-card shadow="hover">
          <el-statistic
            :title="t('crawlerSource.keywords')"
            :value="s.active_keywords || 0"
          />
        </el-card>
      </el-col>
      <el-col :span="3">
        <el-card shadow="hover">
          <el-statistic
            :title="t('crawlerSource.engineGroups')"
            :value="s.active_engines || 0"
          />
        </el-card>
      </el-col>
      <el-col :span="3">
        <el-card shadow="hover">
          <el-statistic
            :title="t('crawlerSource.domains')"
            :value="s.active_domains || 0"
          />
        </el-card>
      </el-col>
    </el-row>

    
    <el-card class="mb-4">
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-bold">{{ t('crawlerSource.domainTitle') }}</span>
          <div class="flex items-center gap-2">
            <el-button
              size="small"
              type="primary"
              data-testid="crawler-refresh"
              :loading="loading"
              @click="loadAll"
            >
              {{ t('crawlerSource.refresh') }}
            </el-button>
            <el-button
              size="small"
              type="success"
              :loading="running"
              data-testid="crawler-run"
              @click="triggerCrawl"
            >
              {{ t('crawlerSource.run') }}
            </el-button>
          </div>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="domainCompare"
        size="default"
        :default-sort="{prop:'probe_count', order:'descending'}"
      >
        <el-table-column
          :label="t('crawlerSource.rank')"
          width="60"
        >
          <template #default="{ $index }">
            <span class="font-bold">{{ $index + 1 }}</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="t('crawlerSource.domain')"
          min-width="240"
        >
          <template #default="{ row }">
            <div class="flex items-center gap-2">
              <el-tag
                v-if="isSelfSite(row)"
                type="success"
                size="small"
                effect="dark"
                round
              >
                {{ t('crawlerSource.selfSite') }}
              </el-tag>
              <el-tag
                v-else
                type="info"
                size="small"
                effect="plain"
                round
              >
                {{ t('crawlerSource.competitor') }}
              </el-tag>
              <span
                class="font-mono"
                :class="isSelfSite(row) ? 'font-bold' : ''"
              >{{ row.domain }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="probe_count"
          :label="t('crawlerSource.probeCount')"
          width="120"
          sortable
        >
          <template #default="{ row }">
            <span class="font-mono font-bold text-lg">{{ row.probe_count }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="engines"
          :label="t('crawlerSource.engineGroups')"
          width="120"
        >
          <template #default="{ row }">
            <el-progress
              v-if="configuredGroups > 0"
              :percentage="Math.min(100, Math.round(row.engines / configuredGroups * 100))"
              :stroke-width="10"
              :color="row.engines >= 6 ? '#22c55e' : row.engines >= 4 ? '#eab308' : '#ef4444'"
            />
            <div class="text-xs text-gray-500 mt-1">
              {{ row.engines }} / {{ configuredGroups }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          :label="t('crawlerSource.probeShare')"
          width="200"
        >
          <template #default="{ row }">
            <div class="flex items-center gap-2">
              <el-progress
                :percentage="Math.round(row.probe_share_pct || 0)"
                :stroke-width="12"
                :color="isSelfSite(row) ? '#10b981' : '#6366f1'"
                style="flex:1"
              />
              <span class="font-mono font-bold w-14 text-right">{{ Number(row.probe_share_pct || 0).toFixed(1) }}%</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="source_level"
          :label="t('crawlerSource.configuredLevel')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="levelType(row.source_level)"
              size="small"
            >
              {{ row.source_level || 'D' }}
            </el-tag>
          </template>
        </el-table-column>
        <template #empty>
          <div class="py-8 text-gray-400">
            {{ t('crawlerSource.empty') }}
          </div>
        </template>
      </el-table>
    </el-card>

    
    <el-card class="mb-4">
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-bold">{{ t('crawlerSource.keywordTitle') }}</span>
          <el-input
            v-model="kwFilter"
            :placeholder="t('crawlerSource.keywordFilter')"
            :aria-label="t('crawlerSource.keywordFilter')"
            size="small"
            style="width:160px"
            clearable
          />
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="pagedKeywordStats"
        size="small"
        :default-sort="{prop:'probe_count', order:'descending'}"
      >
        <el-table-column
          prop="keyword"
          :label="t('crawlerSource.keyword')"
          min-width="220"
        >
          <template #default="{ row }">
            <span class="font-semibold">{{ row.keyword }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="engine"
          :label="t('crawlerSource.engine')"
          width="160"
        >
          <template #default="{ row }">
            <el-tag
              size="small"
              :type="engineTagType(row.engine)"
            >
              {{ row.engine }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="probe_count"
          :label="t('crawlerSource.probeCount')"
          width="100"
          sortable
        >
          <template #default="{ row }">
            <span class="font-mono font-bold">{{ row.probe_count }}</span>
          </template>
        </el-table-column>
        <el-table-column
          :label="t('crawlerSource.relativeCount')"
          min-width="240"
        >
          <template #default="{ row }">
            <div
              class="heat-bar"
              :style="{ width: heatWidth(row.probe_count) + '%', background: heatColor(row.probe_count) }"
            />
          </template>
        </el-table-column>
      </el-table>
      <!-- 关键词行数可达数千（现网 7001 行），必须分页渲染，否则整表单次挂载会把页面卡住 -->
      <el-pagination
        v-model:current-page="kwPage"
        :page-size="kwPageSize"
        :total="filteredKeywordStats.length"
        layout="prev, pager, next, total"
        small
      />
    </el-card>

    
    <el-card>
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-bold">{{ t('crawlerSource.detailTitle') }}</span>
          <el-tag
            size="small"
            type="info"
          >
            {{ t('crawlerSource.debugOnly') }}
          </el-tag>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="domainStats"
        size="small"
        :default-sort="{prop:'probe_count', order:'descending'}"
      >
        <el-table-column
          prop="domain"
          :label="t('crawlerSource.domain')"
          min-width="200"
        >
          <template #default="{ row }">
            <span class="font-mono">{{ row.domain }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="engine"
          :label="t('crawlerSource.engine')"
          width="160"
        />
        <el-table-column
          prop="probe_count"
          :label="t('crawlerSource.probeCount')"
          width="100"
          sortable
        />
        <el-table-column
          prop="source_level"
          :label="t('crawlerSource.configuredLevel')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="levelType(row.source_level)"
              size="small"
            >
              {{ row.source_level || 'D' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { getCrawlerStats, runCrawler as runCrawlerApi } from '@/api/geoProbe.js'

const { t } = useI18n()
const loading = ref(false)
const running = ref(false)
const keywordStats = ref([])
const domainStats = ref([])
const domainCompare = ref([])
const coverageScore = ref(0)
const s = ref({})
const kwFilter = ref('')
const sourceMeta = ref(null)
const unattributed = ref(false)
const loadFailed = ref(false)
const configuredGroups = computed(() => Number(sourceMeta.value?.configured_engine_groups) || 0)
// 规范字段优先；只在 API 已确认来源为主动探测后兼容旧站点标记。
const isSelfSite = (row) => (row.is_self_site ?? row.is_hivemtk) === true

const filteredKeywordStats = computed(() => {
  if (!kwFilter.value) return keywordStats.value
  return keywordStats.value.filter(k => k.keyword.includes(kwFilter.value))
})

// 关键词热力表分页：后端一次返回上千行（现网 7001 行），直接整表渲染会明显卡顿
const kwPage = ref(1)
const kwPageSize = 50
const pagedKeywordStats = computed(() => {
  const start = (kwPage.value - 1) * kwPageSize
  return filteredKeywordStats.value.slice(start, start + kwPageSize)
})
// 过滤条件变化后回到第 1 页，避免停留在超出结果的页码上看到空表
watch(kwFilter, () => { kwPage.value = 1 })

const coverageColor = computed(() => {
  const v = coverageScore.value
  if (v >= 70) return '#10b981'
  if (v >= 50) return '#f59e0b'
  return '#ef4444'
})

const maxCount = computed(() => {
  const all = [...keywordStats.value, ...domainStats.value].map(d => d.probe_count || 0)
  return Math.max(1, ...all)
})
const heatWidth = (c) => Math.min(100, Math.round(((c || 0) / maxCount.value) * 100))
const heatColor = (c) => {
  const ratio = (c || 0) / maxCount.value
  if (ratio > 0.8) return '#ef4444'
  if (ratio > 0.6) return '#f59e0b'
  if (ratio > 0.3) return '#eab308'
  return '#22c55e'
}
const levelType = (l) => ({ A: 'success', B: 'primary', C: 'warning', D: 'info' }[l] || 'info')
const engineTagType = (e) => {
  if (e.includes('GPT')) return 'primary'
  if (e.includes('Claude')) return 'warning'
  if (e.includes('Perplexity')) return 'success'
  if (e.includes('Google')) return 'danger'
  return 'info'
}

const loadAll = async () => {
  if (loading.value) return
  loading.value = true
  loadFailed.value = false
  unattributed.value = false
  s.value = {}
  sourceMeta.value = null
  keywordStats.value = []
  domainStats.value = []
  domainCompare.value = []
  coverageScore.value = 0
  try {
    const data = await getCrawlerStats()
    if (data?.data_source?.kind !== 'active_ua_probe') {
      unattributed.value = true
      return
    }
    sourceMeta.value = data.data_source
    s.value = data?.summary || {}
    keywordStats.value = data?.keyword_stats || []
    domainStats.value = data?.domain_stats || []
    domainCompare.value = data?.domain_compare || []
    coverageScore.value = data?.self_site_probe_share_pct || 0
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

const triggerCrawl = async () => {
  if (running.value) return
  running.value = true
  try {
    await runCrawlerApi()
    ElMessage.success(t('crawlerSource.accepted'))
  } catch {
    ElMessage.error(t('crawlerSource.runFailed'))
  } finally {
    running.value = false
  }
}

onMounted(loadAll)
</script>

<style lang="scss" scoped>
.coverage-card {
  background: linear-gradient(135deg, #f0fdf4 0%, #ecfdf5 100%);
  border-color: #86efac;
}
.heat-bar {
  height: 16px;
  border-radius: 4px;
  transition: width .4s ease;
}
</style>
