<template>
  <div class="index-tracking">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>🔍 收录与 AI 引用追踪</span>
          <div>
            <el-button
              type="primary"
              @click="refreshActive"
            >
              刷新
            </el-button>
            <el-button @click="verifyAll">
              一键全量验证
            </el-button>
          </div>
        </div>
      </template>

      <el-tabs v-model="activeTab">
        <el-tab-pane
          label="收录列表"
          name="index"
        >
          <el-empty
            v-if="!trackings.length"
            description="暂无收录数据"
          />
          <el-table
            v-else
            :data="trackings"
            size="small"
            border
          >
            <el-table-column
              prop="url"
              label="URL"
              show-overflow-tooltip
            />
            <el-table-column
              prop="engine"
              label="引擎"
              width="100"
            />
            <el-table-column
              label="收录"
              width="80"
            >
              <template #default="{ row }">
                <el-tag
                  :type="row.indexed ? 'success' : 'info'"
                  size="small"
                >
                  {{ row.indexed ? '✅' : '—' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="rank_position"
              label="排名"
              width="80"
            />
            <el-table-column
              label="AI 引用"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  :type="row.ai_cited ? 'success' : 'warning'"
                  size="small"
                >
                  {{ row.ai_cited ? '✅ ' + row.ai_cite_count + '次' : '❌' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="last_checked"
              label="验证时间"
              width="160"
            />
          </el-table>
        </el-tab-pane>

        <el-tab-pane
          label="引用漂移"
          name="drift"
        >
          <el-alert
            type="info"
            :closable="false"
            show-icon
            title="对比两次按日快照：新增 / 消失 / 计数变化 / 排名变化。没有快照就如实报“无快照”，不冒充“没有漂移”。"
            style="margin-bottom: 12px"
          />
          <div class="toolbar">
            <el-date-picker
              v-model="driftFrom"
              type="date"
              placeholder="起始日期（留空=上一个快照）"
              value-format="YYYY-MM-DD"
              style="width: 240px"
            />
            <el-date-picker
              v-model="driftTo"
              type="date"
              placeholder="目标日期（留空=今天）"
              value-format="YYYY-MM-DD"
              style="width: 240px"
            />
            <el-button
              type="primary"
              :loading="driftLoading"
              @click="loadDrift"
            >
              对比
            </el-button>
            <span
              v-if="drift"
              class="muted"
            >{{ drift.from_date }} → {{ drift.to_date }}</span>
          </div>

          <el-alert
            v-if="driftError"
            type="warning"
            :closable="false"
            show-icon
            :title="driftError"
            style="margin-bottom: 12px"
          />

          <el-row
            v-if="drift"
            :gutter="12"
            class="stat-row"
          >
            <el-col :span="6">
              <el-statistic
                title="新增引用"
                :value="(drift.added || []).length"
              />
            </el-col>
            <el-col :span="6">
              <el-statistic
                title="消失引用"
                :value="(drift.removed || []).length"
              />
            </el-col>
            <el-col :span="6">
              <el-statistic
                title="计数变化"
                :value="(drift.changed || []).length"
              />
            </el-col>
            <el-col :span="6">
              <el-statistic
                title="排名变化"
                :value="(drift.rank_moves || []).length"
              />
            </el-col>
          </el-row>

          <template v-if="drift">
            <div class="section-title">
              新增引用
            </div>
            <el-table
              :data="drift.added || []"
              size="small"
              border
              empty-text="无新增"
            >
              <el-table-column
                prop="domain"
                label="域名"
                width="200"
              />
              <el-table-column
                prop="url"
                label="URL"
                show-overflow-tooltip
              />
              <el-table-column
                prop="count"
                label="引用次数"
                width="100"
              />
            </el-table>

            <div class="section-title">
              消失引用
            </div>
            <el-table
              :data="drift.removed || []"
              size="small"
              border
              empty-text="无消失"
            >
              <el-table-column
                prop="domain"
                label="域名"
                width="200"
              />
              <el-table-column
                prop="url"
                label="URL"
                show-overflow-tooltip
              />
              <el-table-column
                prop="count"
                label="原引用次数"
                width="110"
              />
            </el-table>

            <div class="section-title">
              计数变化
            </div>
            <el-table
              :data="drift.changed || []"
              size="small"
              border
              empty-text="无变化"
            >
              <el-table-column
                prop="domain"
                label="域名"
                width="200"
              />
              <el-table-column
                prop="url"
                label="URL"
                show-overflow-tooltip
              />
              <el-table-column
                prop="from"
                label="之前"
                width="80"
              />
              <el-table-column
                prop="to"
                label="之后"
                width="80"
              />
              <el-table-column
                label="增减"
                width="90"
              >
                <template #default="{ row }">
                  <span :class="row.delta >= 0 ? 'up' : 'down'">
                    {{ row.delta > 0 ? '+' : '' }}{{ row.delta }}
                  </span>
                </template>
              </el-table-column>
            </el-table>

            <div class="section-title">
              排名变化
            </div>
            <el-table
              :data="drift.rank_moves || []"
              size="small"
              border
              empty-text="无排名变化"
            >
              <el-table-column
                prop="domain"
                label="域名"
                width="200"
              />
              <el-table-column
                prop="url"
                label="URL"
                show-overflow-tooltip
              />
              <el-table-column
                prop="from_rank"
                label="之前排名"
                width="100"
              />
              <el-table-column
                prop="to_rank"
                label="之后排名"
                width="100"
              />
            </el-table>
          </template>
        </el-tab-pane>

        <el-tab-pane
          label="引用分析"
          name="domains"
        >
          <div class="toolbar">
            <el-select
              v-model="days"
              style="width: 120px"
              @change="loadDomains"
            >
              <el-option
                label="近 7 天"
                :value="7"
              />
              <el-option
                label="近 30 天"
                :value="30"
              />
              <el-option
                label="近 90 天"
                :value="90"
              />
            </el-select>
            <el-button @click="loadDomains">
              刷新
            </el-button>
            <el-button
              :loading="exporting === 'domains'"
              @click="download('domains')"
            >
              导出域名 CSV
            </el-button>
            <el-button
              :loading="exporting === 'matrix'"
              @click="download('matrix')"
            >
              导出矩阵 CSV
            </el-button>
          </div>

          <el-row
            v-if="domains"
            :gutter="12"
            class="stat-row"
          >
            <el-col :span="8">
              <el-statistic
                title="总引用次数"
                :value="domains.total || 0"
              />
            </el-col>
            <el-col :span="8">
              <el-statistic
                title="自有域引用"
                :value="domains.own_citations || 0"
              />
            </el-col>
            <el-col :span="8">
              <el-statistic
                title="自有域覆盖率"
                :value="Math.round((domains.own_coverage || 0) * 1000) / 10"
                suffix="%"
              />
            </el-col>
          </el-row>

          <div class="section-title">
            域名分布
          </div>
          <el-table
            :data="domains?.domains || []"
            size="small"
            border
            max-height="360"
          >
            <el-table-column
              prop="domain"
              label="域名"
              min-width="200"
            />
            <el-table-column
              label="分类"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  size="small"
                  :type="row.category === 'own' ? 'success' : row.category === 'competitor' ? 'warning' : 'info'"
                >
                  {{ categoryText(row.category) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="count"
              label="引用次数"
              width="100"
            />
            <el-table-column
              prop="query_count"
              label="覆盖查询"
              width="100"
            />
            <el-table-column
              prop="engine_count"
              label="引擎数"
              width="90"
            />
          </el-table>

          <div class="section-title">
            Top 外链
          </div>
          <el-table
            :data="domains?.top_external || []"
            size="small"
            border
            empty-text="暂无外链引用"
          >
            <el-table-column
              prop="domain"
              label="域名"
              width="200"
            />
            <el-table-column
              prop="url"
              label="URL"
              show-overflow-tooltip
            />
            <el-table-column
              prop="count"
              label="引用次数"
              width="100"
            />
          </el-table>

          <div class="section-title">
            关键词 × 域名矩阵
          </div>
          <el-table
            :data="matrix?.rows || []"
            size="small"
            border
            max-height="420"
          >
            <el-table-column
              prop="query"
              label="查询"
              min-width="220"
              fixed
              show-overflow-tooltip
            />
            <el-table-column
              v-for="d in matrix?.domains || []"
              :key="d"
              :label="d"
              width="110"
            >
              <template #default="{ row }">
                {{ row.domains?.[d] || 0 }}
              </template>
            </el-table-column>
            <el-table-column
              prop="total"
              label="合计"
              width="80"
            />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { geoApi } from '@/api/geo'
import { ElMessage } from 'element-plus'

const activeTab = ref('index')
const trackings = ref([])

const driftFrom = ref('')
const driftTo = ref('')
const drift = ref(null)
const driftError = ref('')
const driftLoading = ref(false)

const days = ref(30)
const domains = ref(null)
const matrix = ref(null)
const exporting = ref('')

async function load() {
  try {
    const res = await geoApi.getIndexFunnel()
    trackings.value = res?.trackings || []
  } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
}

async function verifyAll() {
  try {
    await geoApi.verifyAllIndex()
    ElMessage.success('已触发全量验证')
    load()
  } catch (e) { ElMessage.error('失败') }
}

function refreshActive () {
  if (activeTab.value === 'drift') return loadDrift()
  if (activeTab.value === 'domains') return loadDomains()
  return load()
}

async function loadDrift () {
  driftLoading.value = true
  driftError.value = ''
  try {
    drift.value = await geoApi.getCitationDrift({
      from: driftFrom.value || '',
      to: driftTo.value || ''
    })
  } catch (e) {
    drift.value = null
    driftError.value = e?.message || '没有可比对的快照'
  } finally {
    driftLoading.value = false
  }
}

async function loadDomains () {
  try {
    const [d, m] = await Promise.all([
      geoApi.getCitationDomains(days.value),
      geoApi.getCitationMatrix(days.value, 20, 10)
    ])
    domains.value = d || null
    matrix.value = m || null
  } catch (e) {
    if (e?.message) ElMessage.error(e.message)
  }
}

async function download (kind) {
  exporting.value = kind
  try {
    const params = kind === 'matrix'
      ? { days: days.value, top_queries: 20, top_domains: 10 }
      : { days: days.value }
    const blob = await geoApi.downloadCitationCSV(kind, params)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = kind === 'matrix' ? 'citation-matrix.csv' : 'citation-domains.csv'
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error(e?.message || '导出失败')
  } finally {
    exporting.value = ''
  }
}

function categoryText (c) {
  if (c === 'own') return '自有'
  if (c === 'competitor') return '竞品'
  return '外部'
}

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.muted {
  color: #6b7280;
  font-size: 12px;
}
.stat-row {
  margin-bottom: 12px;
}
.section-title {
  font-size: 14px;
  font-weight: 600;
  margin: 16px 0 8px;
}
.up {
  color: #047857;
}
.down {
  color: #b91c1c;
}
</style>
