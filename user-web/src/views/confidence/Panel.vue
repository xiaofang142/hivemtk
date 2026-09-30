<template>
  <div class="confidence-panel">
    <el-card class="header-card">
      <div class="header-content">
        <h2>{{ $t('置信度运营面板') }}</h2>
        <p class="subtitle">{{ $t('查看置信度信号聚合分布、动态阈值策略、转人工规则') }}</p>
      </div>
    </el-card>

    <el-row :gutter="20" class="stats-row">
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">信号总数（24h）</div>
            <div class="stat-value">{{ stats.totalSignals }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('平均置信度') }}</div>
            <div class="stat-value" :style="{ color: avgConfColor }">{{ stats.avgConfidence }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('转人工触发') }}</div>
            <div class="stat-value" style="color: #F59E0B">{{ stats.transferCount }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('高置信自动回复') }}</div>
            <div class="stat-value" style="color: #10B981">{{ stats.autoReplyCount }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-tabs v-model="activeTab" class="content-tabs">
      
      <el-tab-pane :label="$t('置信度信号')" name="signals">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>信号流（最近 100 条）</span>
              <div>
                <el-input v-model="signalSearch" :placeholder="$t('搜索 session_id')" clearable style="width: 200px" @change="loadSignals" />
                <el-button @click="loadSignals" style="margin-left: 10px">
                  <el-icon><Refresh /></el-icon>
                  {{ $t('刷新') }}
                </el-button>
              </div>
            </div>
          </template>
          <el-table :data="signals" v-loading="signalsLoading" stripe>
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column prop="session_id" label="会话 ID" width="160" />
            <el-table-column prop="intent_conf" label="意图置信" width="100">
              <template #default="{ row }">
                <el-tag :type="confTagType(row.intent_conf)">{{ (row.intent_conf || 0).toFixed(3) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="entity_comp" label="实体完整度" width="100">
              <template #default="{ row }">
                {{ (row.entity_comp || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="ctx_relev" label="上下文相关" width="100">
              <template #default="{ row }">
                {{ (row.ctx_relev || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="rag_qual" label="RAG 质量" width="100">
              <template #default="{ row }">
                {{ (row.rag_qual || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="llm_entropy" label="LLM 熵" width="100">
              <template #default="{ row }">
                {{ (row.llm_entropy || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="aggregated_score" label="综合置信度" width="110">
              <template #default="{ row }">
                <el-tag :type="confTagType(row.aggregated_score)" effect="dark">
                  {{ (row.aggregated_score || 0).toFixed(3) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="threshold" label="动态阈值" width="100">
              <template #default="{ row }">
                {{ (row.threshold || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column label="决策" width="100">
              <template #default="{ row }">
                <el-tag :type="decisionTagType(row.decision)">{{ decisionLabel(row.decision) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="calculated_at" label="计算时间" width="170">
              <template #default="{ row }">
                {{ formatTime(row.calculated_at) }}
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            v-model:current-page="signalPage"
            v-model:page-size="signalPageSize"
            :total="signalTotal"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            @current-change="loadSignals"
            @size-change="loadSignals"
            style="margin-top: 16px; text-align: right"
          />
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="置信度校准" name="calibrations">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>温度缩放校准记录（最近 50 条）</span>
              <el-button @click="loadCalibrations">
                <el-icon><Refresh /></el-icon>
                刷新
              </el-button>
            </div>
          </template>
          <el-table :data="calibrations" v-loading="calibrationsLoading" stripe>
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column prop="scenario" label="场景" width="160" />
            <el-table-column prop="temperature" label="温度参数" width="120">
              <template #default="{ row }">
                {{ (row.temperature || 0).toFixed(4) }}
              </template>
            </el-table-column>
            <el-table-column prop="ece" label="ECE（期望校准误差）" width="180">
              <template #default="{ row }">
                <el-tag :type="row.ece < 0.05 ? 'success' : row.ece < 0.1 ? 'warning' : 'danger'">
                  {{ (row.ece || 0).toFixed(4) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="sample_count" label="样本数" width="100" />
            <el-table-column prop="calculated_at" label="计算时间" width="170">
              <template #default="{ row }">
                {{ formatTime(row.calculated_at) }}
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="转人工阈值策略" name="policies">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>转人工规则（base / customer_level / timeslot / agent_availability 四因子动态阈值）</span>
              <el-button type="primary" @click="showPolicyDialog()">
                <el-icon><Plus /></el-icon>
                新建策略
              </el-button>
            </div>
          </template>
          <el-table :data="policies" v-loading="policiesLoading" stripe>
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column prop="name" label="策略名" />
            <el-table-column prop="scenario" label="适用场景" width="160" />
            <el-table-column prop="base_threshold" label="基础阈值" width="100">
              <template #default="{ row }">
                {{ (row.base_threshold || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="vip_threshold" label="VIP 阈值" width="100">
              <template #default="{ row }">
                {{ (row.vip_threshold || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="transfer_low" label="远低转人工" width="100">
              <template #default="{ row }">
                {{ (row.transfer_low || 0).toFixed(3) }}
              </template>
            </el-table-column>
            <el-table-column prop="is_active" label="启用" width="80">
              <template #default="{ row }">
                <el-switch :model-value="row.is_active" disabled />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="160" fixed="right">
              <template #default="{ row }">
                <el-button size="small" @click="showPolicyDialog(row)">编辑</el-button>
                <el-button size="small" type="primary" @click="togglePolicy(row)">
                  {{ row.is_active ? '禁用' : '启用' }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog v-model="policyDialogVisible" :title="editingPolicy.id ? '编辑阈值策略' : '新建阈值策略'" width="640px">
      <el-form :model="editingPolicy" label-width="120px">
        <el-form-item label="策略名" required>
          <el-input v-model="editingPolicy.name" placeholder="例如：电商_高客单价场景" />
        </el-form-item>
        <el-form-item label="适用场景">
          <el-select v-model="editingPolicy.scenario" placeholder="选择场景" style="width: 100%">
            <el-option label="电商高客单" value="ecommerce_high_value" />
            <el-option label="电商低客单" value="ecommerce_low_value" />
            <el-option label="SaaS 销售" value="saas_sales" />
            <el-option label="客服咨询" value="customer_service" />
            <el-option label="全场景" value="all" />
          </el-select>
        </el-form-item>
        <el-form-item label="基础阈值">
          <el-input-number v-model="editingPolicy.base_threshold" :min="0" :max="1" :step="0.01" />
          <span class="form-tip">默认 0.7，置信度低于此值触发 LLM 兜底</span>
        </el-form-item>
        <el-form-item label="VIP 阈值">
          <el-input-number v-model="editingPolicy.vip_threshold" :min="0" :max="1" :step="0.01" />
          <span class="form-tip">VIP 客户阈值更高（更倾向转人工）</span>
        </el-form-item>
        <el-form-item label="远低转人工">
          <el-input-number v-model="editingPolicy.transfer_low" :min="0" :max="1" :step="0.01" />
          <span class="form-tip">低于此值直接转人工，不进 LLM 兜底</span>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="editingPolicy.is_active" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="editingPolicy.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="policyDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="savePolicy">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getConfidenceSignals,
  getConfidenceSignalStats,
  getConfidenceCalibrations,
  getThresholdPolicies,
  upsertThresholdPolicy
} from '@/api/tuning'

const activeTab = ref('signals')

const stats = ref({ totalSignals: 0, avgConfidence: '0.000', transferCount: 0, autoReplyCount: 0 });
const avgConfColor = computed(() => {
  const v = parseFloat(stats.value.avgConfidence)
  if (v >= 0.7) return '#10B981'
  if (v >= 0.5) return '#F59E0B'
  return '#EF4444'
})

const signals = ref([]);
const signalSearch = ref('')
const signalPage = ref(1)
const signalPageSize = ref(20)
const signalTotal = ref(0)
const signalsLoading = ref(false)
async function loadSignals() {
  signalsLoading.value = true
  try {
    const params = { page: signalPage.value, page_size: signalPageSize.value }
    if (signalSearch.value) params.session_id = signalSearch.value
    const res = await getConfidenceSignals(params)
    signals.value = res?.list || res?.data?.list || []
    signalTotal.value = res?.total || res?.data?.total || 0
  } catch (e) {
    ElMessage.error('信号列表加载失败：' + (e?.message || e))
  } finally {
    signalsLoading.value = false
  }
}

async function loadStats() {
  try {
    const res = await getConfidenceSignalStats({ range: '24h' })
    const data = res?.data || res || {}
    stats.value = {
      totalSignals: data.total || 0,
      avgConfidence: (data.avg_score || 0).toFixed(3),
      transferCount: data.transfer_count || 0,
      autoReplyCount: data.auto_reply_count || 0
    }
  } catch (e) {
    stats.value = { totalSignals: 0, avgConfidence: '0.000', transferCount: 0, autoReplyCount: 0 };
  }
}

const calibrations = ref([]);
const calibrationsLoading = ref(false)
async function loadCalibrations() {
  calibrationsLoading.value = true
  try {
    const res = await getConfidenceCalibrations({ page: 1, page_size: 50 })
    calibrations.value = res?.list || res?.data?.list || []
  } catch (e) {
    ElMessage.error('校准数据加载失败：' + (e?.message || e))
  } finally {
    calibrationsLoading.value = false
  }
}

const policies = ref([]);
const policiesLoading = ref(false)
async function loadPolicies() {
  policiesLoading.value = true
  try {
    const res = await getThresholdPolicies({ page: 1, page_size: 100 })
    policies.value = res?.list || res?.data?.list || []
  } catch (e) {
    ElMessage.error('策略列表加载失败：' + (e?.message || e))
  } finally {
    policiesLoading.value = false
  }
}

const policyDialogVisible = ref(false)
const editingPolicy = ref({
  id: 0,
  name: '',
  scenario: 'all',
  base_threshold: 0.7,
  vip_threshold: 0.85,
  transfer_low: 0.3,
  is_active: true,
  remark: ''
})
function showPolicyDialog(row) {
  if (row) {
    editingPolicy.value = { ...row }
  } else {
    editingPolicy.value = {
      id: 0,
      name: '',
      scenario: 'all',
      base_threshold: 0.7,
      vip_threshold: 0.85,
      transfer_low: 0.3,
      is_active: true,
      remark: ''
    }
  }
  policyDialogVisible.value = true
}
async function savePolicy() {
  if (!editingPolicy.value.name) {
    ElMessage.warning(i18n.global.t('请填写策略名'))
    return
  }
  try {
    await upsertThresholdPolicy(editingPolicy.value)
    ElMessage.success(i18n.global.t('保存成功'))
    policyDialogVisible.value = false
    await loadPolicies()
  } catch (e) {
    ElMessage.error('保存失败：' + (e?.message || e))
  }
}
async function togglePolicy(row) {
  const action = row.is_active ? '禁用' : '启用'
  try {
    await ElMessageBox.confirm(`确认要${action}策略「${row.name}」？`, '提示', { type: 'warning' })
    await upsertThresholdPolicy({ ...row, is_active: !row.is_active })
    ElMessage.success(`${action}成功`)
    await loadPolicies()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') {
      ElMessage.error(`${action}失败：` + (e?.message || e))
    }
  }
}

function confTagType(v) {
  if (v >= 0.7) return 'success'
  if (v >= 0.5) return 'warning'
  return 'danger'
}
function decisionLabel(d) {
  return { auto_reply: '自动回复', llm_fallback: 'LLM 兜底', review_queue: '审核队列', transfer: '转人工' }[d] || d || '-'
}
function decisionTagType(d) {
  // 兜底不能用空串：el-tag 的 type 合法值只有 primary/success/info/warning/danger，
  // 传 '' 会触发 `Invalid prop: validation failed for prop "type"` 告警，
  // 并回落到 default(primary) 显示成蓝色，与「未识别的决策」这一中性语义不符。
  return { auto_reply: 'success', llm_fallback: 'info', review_queue: 'warning', transfer: 'danger' }[d] || 'info'
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
  await Promise.all([loadSignals(), loadStats(), loadCalibrations(), loadPolicies()])
})
</script>

<style scoped>
.confidence-panel { padding: 16px; }
.header-card { margin-bottom: 16px; }
.header-content h2 { margin: 0 0 4px 0; font-size: 20px; }
.subtitle { color: #909399; margin: 0; font-size: 13px; }
.stats-row { margin-bottom: 16px; }
.stat-item { text-align: center; }
.stat-label { color: #909399; font-size: 12px; margin-bottom: 8px; }
.stat-value { font-size: 28px; font-weight: 600; color: #303133; }
.content-tabs { background: #fff; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.form-tip { margin-left: 12px; color: #909399; font-size: 12px; }
</style>
