<template>
  <div class="humanize-panel">
    <el-card class="header-card">
      <div class="header-content">
        <h2>拟人度评估 & 销冠基线</h2>
        <p class="subtitle">
          查看 AI 回复拟人度评分、销冠话术基线、低质样本收集
        </p>
      </div>
    </el-card>

    <el-row
      :gutter="20"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              评估总数（24h）
            </div>
            <div class="stat-value">
              {{ stats.totalScored }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('平均拟人度') }}
            </div>
            <div
              class="stat-value"
              :style="{ color: avgScoreColor }"
            >
              {{ stats.avgScore }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('低质样本') }}
            </div>
            <div
              class="stat-value"
              style="color: #EF4444"
            >
              {{ stats.lowQualityCount }}
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">
              {{ $t('销冠基线') }}
            </div>
            <div
              class="stat-value"
              style="color: #10B981"
            >
              {{ stats.baselineCount }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-tabs
      v-model="activeTab"
      class="content-tabs"
    >
      <el-tab-pane
        :label="$t('评估结果')"
        name="scores"
      >
        <el-card>
          <template #header>
            <div class="card-header">
              <span>拟人度评分流（最近 100 条）</span>
              <el-button @click="loadScores">
                <el-icon><Refresh /></el-icon>
                {{ $t('刷新') }}
              </el-button>
            </div>
          </template>
          <el-table
            v-loading="scoresLoading"
            :data="scores"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              prop="session_id"
              label="会话 ID"
              width="160"
            />
            <el-table-column
              prop="rule_score"
              label="规则评分"
              width="100"
            >
              <template #default="{ row }">
                <el-tag :type="confTagType(row.rule_score)">
                  {{ (row.rule_score || 0).toFixed(3) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="llm_score"
              label="LLM 评分"
              width="100"
            >
              <template #default="{ row }">
                {{ (row.llm_score || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column
              prop="final_score"
              label="综合评分"
              width="100"
            >
              <template #default="{ row }">
                <el-tag
                  :type="confTagType(row.final_score)"
                  effect="dark"
                >
                  {{ (row.final_score || 0).toFixed(3) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="regenerated"
              label="是否重生成"
              width="100"
            >
              <template #default="{ row }">
                <el-tag :type="row.regenerated ? 'warning' : 'info'">
                  {{ row.regenerated ? '是' : '否' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="issues"
              label="问题标签"
              min-width="180"
            >
              <template #default="{ row }">
                <el-tag
                  v-for="tag in (row.issues || [])"
                  :key="tag"
                  size="small"
                  type="danger"
                  style="margin-right: 4px"
                >
                  {{ tag }}
                </el-tag>
                <span
                  v-if="!row.issues || row.issues.length === 0"
                  class="muted"
                >-</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="created_at"
              label="评分时间"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.created_at) }}
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            :current-page="scorePage"
            :page-size="scorePageSize"
            :total="scoreTotal"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            style="margin-top: 16px; text-align: right"
            @current-change="loadScores"
            @size-change="loadScores"
          />
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane
        label="销冠基线"
        name="baselines"
      >
        <el-card>
          <template #header>
            <div class="card-header">
              <span>销冠话术基线（按场景）</span>
              <el-button @click="loadBaselines">
                <el-icon><Refresh /></el-icon>
                刷新
              </el-button>
            </div>
          </template>
          <el-table
            v-loading="baselinesLoading"
            :data="baselines"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              prop="scenario"
              label="场景"
              width="160"
            />
            <el-table-column
              prop="staff_id"
              label="销冠 ID"
              width="100"
            />
            <el-table-column
              prop="avg_reward"
              label="平均奖励"
              width="100"
            >
              <template #default="{ row }">
                <el-tag :type="confTagType(row.avg_reward)">
                  {{ (row.avg_reward || 0).toFixed(3) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="dialogue_count"
              label="对话数"
              width="100"
            />
            <el-table-column
              prop="scripts"
              label="提取话术数"
              width="100"
            >
              <template #default="{ row }">
                {{ (row.scripts || []).length }}
              </template>
            </el-table-column>
            <el-table-column
              prop="updated_at"
              label="更新时间"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.updated_at) }}
              </template>
            </el-table-column>
            <el-table-column
              label="操作"
              width="120"
              fixed="right"
            >
              <template #default="{ row }">
                <el-button
                  size="small"
                  @click="viewBaselineScripts(row)"
                >
                  查看话术
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane
        label="低质样本"
        name="lowQuality"
      >
        <el-card>
          <template #header>
            <div class="card-header">
              <span>拟人度不达标的 AI 回复（用于反馈学习）</span>
              <el-button @click="loadLowQuality">
                <el-icon><Refresh /></el-icon>
                刷新
              </el-button>
            </div>
          </template>
          <el-table
            v-loading="lowQualityLoading"
            :data="lowQuality"
            stripe
          >
            <el-table-column
              prop="id"
              label="ID"
              width="80"
            />
            <el-table-column
              prop="session_id"
              label="会话 ID"
              width="160"
            />
            <el-table-column
              prop="score"
              label="评分"
              width="100"
            >
              <template #default="{ row }">
                <el-tag type="danger">
                  {{ (row.score || 0).toFixed(3) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column
              prop="ai_reply"
              label="AI 回复"
              min-width="320"
              show-overflow-tooltip
            />
            <el-table-column
              prop="issues"
              label="问题标签"
              min-width="180"
            >
              <template #default="{ row }">
                <el-tag
                  v-for="tag in (row.issues || [])"
                  :key="tag"
                  size="small"
                  type="danger"
                  style="margin-right: 4px"
                >
                  {{ tag }}
                </el-tag>
                <span
                  v-if="!row.issues || row.issues.length === 0"
                  class="muted"
                >-</span>
              </template>
            </el-table-column>
            <el-table-column
              prop="created_at"
              label="采集时间"
              width="170"
            >
              <template #default="{ row }">
                {{ formatTime(row.created_at) }}
              </template>
            </el-table-column>
          </el-table>
          <el-pagination
            :current-page="lowQualityPage"
            :page-size="lowQualityPageSize"
            :total="lowQualityTotal"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            style="margin-top: 16px; text-align: right"
            @current-change="loadLowQuality"
            @size-change="loadLowQuality"
          />
        </el-card>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog
      v-model="scriptsDialogVisible"
      title="销冠基线话术"
      width="720px"
    >
      <div v-if="currentBaseline">
        <p><strong>场景：</strong>{{ currentBaseline.scenario }} · <strong>销冠：</strong>{{ currentBaseline.staff_id }} · <strong>平均奖励：</strong>{{ (currentBaseline.avg_reward || 0).toFixed(3) }}</p>
        <el-divider />
        <div
          v-for="(s, i) in (currentBaseline.scripts || [])"
          :key="i"
          class="script-item"
        >
          <h4>{{ s.title || '话术 ' + (i + 1) }}</h4>
          <p class="script-content">
            {{ s.content }}
          </p>
          <p
            v-if="s.tags"
            class="muted"
          >
            标签：{{ (s.tags || []).join('、') }}
          </p>
          <el-divider v-if="i < (currentBaseline.scripts || []).length - 1" />
        </div>
        <el-empty
          v-if="!currentBaseline.scripts || currentBaseline.scripts.length === 0"
          description="暂无话术"
        />
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import _i18n from '@/i18n'

import { ref, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import {
  getHumanizeScores,
  getHumanizeScoreStats,
  getChampionBaselines,
  getLowQualitySamples
} from '@/api/tuning'

const activeTab = ref('scores')

const stats = ref({ totalScored: 0, avgScore: '0.000', lowQualityCount: 0, baselineCount: 0 });
const avgScoreColor = computed(() => {
  const v = parseFloat(stats.value.avgScore)
  if (v >= 0.7) return '#10B981'
  if (v >= 0.5) return '#F59E0B'
  return '#EF4444'
})

const scores = ref([]);
const scorePage = ref(1)
const scorePageSize = ref(20)
const scoreTotal = ref(0)
const scoresLoading = ref(false)
async function loadScores() {
  scoresLoading.value = true
  try {
    const res = await getHumanizeScores({ page: scorePage.value, page_size: scorePageSize.value })
    scores.value = res?.list || res?.data?.list || []
    scoreTotal.value = res?.total || res?.data?.total || 0
  } catch (e) {
    ElMessage.error('评分加载失败：' + (e?.message || e))
  } finally {
    scoresLoading.value = false
  }
}

const baselines = ref([]);
const baselinesLoading = ref(false)
async function loadBaselines() {
  baselinesLoading.value = true
  try {
    const res = await getChampionBaselines({ page: 1, page_size: 50 })
    baselines.value = res?.list || res?.data?.list || []
  } catch (e) {
    ElMessage.error('基线加载失败：' + (e?.message || e))
  } finally {
    baselinesLoading.value = false
  }
}

const lowQuality = ref([]);
const lowQualityPage = ref(1)
const lowQualityPageSize = ref(20)
const lowQualityTotal = ref(0)
const lowQualityLoading = ref(false)
async function loadLowQuality() {
  lowQualityLoading.value = true
  try {
    const res = await getLowQualitySamples({ page: lowQualityPage.value, page_size: lowQualityPageSize.value })
    lowQuality.value = res?.list || res?.data?.list || []
    lowQualityTotal.value = res?.total || res?.data?.total || 0
  } catch (e) {
    ElMessage.error('低质样本加载失败：' + (e?.message || e))
  } finally {
    lowQualityLoading.value = false
  }
}

async function loadStats() {
  try {
    const res = await getHumanizeScoreStats({ range: '24h' })
    const data = res?.data || res || {}
    stats.value = {
      totalScored: data.total || 0,
      avgScore: (data.avg_score || 0).toFixed(3),
      lowQualityCount: data.low_quality_count || 0,
      baselineCount: baselines.value.length || 0
    }
  } catch  {
    stats.value = { totalScored: 0, avgScore: '0.000', lowQualityCount: 0, baselineCount: 0 }
  }
}

const scriptsDialogVisible = ref(false);
const currentBaseline = ref(null)
function viewBaselineScripts(row) {
  currentBaseline.value = row
  scriptsDialogVisible.value = true
}

function confTagType(v) {
  if (v >= 0.7) return 'success'
  if (v >= 0.5) return 'warning'
  return 'danger'
}
function formatTime(t) {
  if (!t) return '-'
  try {
    return new Date(t).toLocaleString('zh-CN', { hour12: false })
  } catch {
    return t
  }
}

onMounted(async () => {
  await Promise.all([loadScores(), loadBaselines(), loadLowQuality()])
  await loadStats()
})
</script>

<style scoped>
.humanize-panel { padding: 16px; }
.header-card { margin-bottom: 16px; }
.header-content h2 { margin: 0 0 4px 0; font-size: 20px; }
.subtitle { color: #909399; margin: 0; font-size: 13px; }
.stats-row { margin-bottom: 16px; }
.stat-item { text-align: center; }
.stat-label { color: #909399; font-size: 12px; margin-bottom: 8px; }
.stat-value { font-size: 28px; font-weight: 600; color: #303133; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.script-item h4 { margin: 0 0 8px 0; color: #303133; }
.script-content { background: #f5f7fa; padding: 12px; border-radius: 4px; line-height: 1.6; }
.muted { color: #909399; font-size: 12px; }
</style>
