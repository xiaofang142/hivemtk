<template>
  <div class="churn-prediction-page">
    <el-card class="header-card">
      <div class="header-content">
        <h2>流失预测</h2>
        <p class="subtitle">基于机器学习预测用户流失风险，提前干预</p>
      </div>
      <div>
        <el-button type="primary" :loading="predicting" @click="runPrediction">
          <el-icon><Operation /></el-icon>
          运行预测
        </el-button>
        <el-button @click="refreshAll">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </el-card>

    <el-tabs v-model="activeTab">
      
      <el-tab-pane label="预测列表" name="predictions">
        <el-row :gutter="20" class="stats-row">
          <el-col :span="6">
            <el-card>
              <div class="stat-item">
                <div class="stat-label">高风险用户</div>
                <div class="stat-value" style="color: #EF4444">{{ stats.highRisk }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card>
              <div class="stat-item">
                <div class="stat-label">中风险用户</div>
                <div class="stat-value" style="color: #F59E0B">{{ stats.mediumRisk }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card>
              <div class="stat-item">
                <div class="stat-label">低风险用户</div>
                <div class="stat-value" style="color: #10B981">{{ stats.lowRisk }}</div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card>
              <div class="stat-item">
                <div class="stat-label">极高风险用户</div>
                <div class="stat-value" style="color: #B91C1C">{{ stats.criticalRisk }}</div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card>
          <template #header>
            <div class="card-header">
              <span>高风险用户列表</span>
              <el-input v-model="searchKeyword" placeholder="搜索用户ID" clearable style="width: 200px" />
            </div>
          </template>
          <el-table :data="filteredUsers" v-loading="loadingPred" stripe>
            <el-table-column prop="user_id" label="用户ID" width="120" />
            <el-table-column prop="churn_score" label="风险评分" width="160">
              <template #default="{ row }">
                <el-progress :percentage="Math.round(row.churn_score || 0)" :color="getRiskColor(row.churn_score)" />
              </template>
            </el-table-column>
            <el-table-column prop="churn_risk" label="风险等级" width="100">
              <template #default="{ row }">
                <el-tag :type="getRiskLevelType(row.churn_risk)">{{ row.churn_risk }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="last_activity_at" label="最后活跃" min-width="180" />
            <el-table-column prop="predicted_at" label="预测时间" min-width="180" />
            <el-table-column prop="risk_factors" label="流失原因" min-width="200" show-overflow-tooltip />
            <el-table-column label="操作" width="180" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="viewUserDetail(row)">详情</el-button>
                <el-button link type="success" @click="intervene(row)">干预</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无数据" />
            </template>
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="预警列表" name="warnings">
        <el-card>
          <el-table :data="warnings" v-loading="loadingWarn" stripe>
            <el-table-column prop="user_id" label="用户ID" width="120" />
            <el-table-column prop="warning_level" label="预警等级" width="100">
              <template #default="{ row }">
                <el-tag :type="warnLevelType(row.warning_level)">{{ row.warning_level }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="warning_type" label="类型" width="140" />
            <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
            <el-table-column prop="suggestion" label="建议措施" min-width="200" show-overflow-tooltip />
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="row.is_handled ? 'success' : 'danger'">
                  {{ row.is_handled ? '已处理' : '未处理' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120" fixed="right">
              <template #default="{ row }">
                <el-button v-if="!row.is_handled" size="small" type="primary" @click="handleWarning(row)">
                  标记已处理
                </el-button>
                <span v-else class="handled-text">已处理</span>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无预警" />
            </template>
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="模型配置" name="config">
        <el-card>
          <el-form :model="configForm" label-width="170px" style="max-width: 640px">
            <el-divider content-position="left">权重配置</el-divider>
            <el-form-item label="未活跃天数权重">
              <el-input-number v-model="configForm.inactive_days_weight" :step="0.05" :min="0" :max="1" />
            </el-form-item>
            <el-form-item label="购买频率权重">
              <el-input-number v-model="configForm.purchase_freq_weight" :step="0.05" :min="0" :max="1" />
            </el-form-item>
            <el-form-item label="订单金额权重">
              <el-input-number v-model="configForm.order_value_weight" :step="0.05" :min="0" :max="1" />
            </el-form-item>
            <el-form-item label="互动频率权重">
              <el-input-number v-model="configForm.engagement_weight" :step="0.05" :min="0" :max="1" />
            </el-form-item>
            <el-divider content-position="left">阈值配置</el-divider>
            <el-form-item label="未活跃阈值(天)">
              <el-input-number v-model="configForm.inactive_threshold" :min="1" :max="365" />
            </el-form-item>
            <el-form-item label="未购买阈值(天)">
              <el-input-number v-model="configForm.purchase_threshold" :min="1" :max="365" />
            </el-form-item>
            <el-form-item label="高风险分数">
              <el-input-number v-model="configForm.high_risk_score" :min="0" :max="100" />
            </el-form-item>
            <el-form-item label="极高风险分数">
              <el-input-number v-model="configForm.critical_risk_score" :min="0" :max="100" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="savingConfig" @click="saveConfig">保存配置</el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog v-model="interveneDialogVisible" title="流失干预" width="500px">
      <el-form :model="interveneForm" label-width="100px">
        <el-form-item label="干预方式">
          <el-select v-model="interveneForm.method" style="width: 100%">
            <el-option label="发送优惠" value="coupon" />
            <el-option label="发送关怀短信" value="sms" />
            <el-option label="人工跟进" value="manual" />
            <el-option label="推送通知" value="push" />
          </el-select>
        </el-form-item>
        <el-form-item label="干预内容">
          <el-input v-model="interveneForm.content" type="textarea" :rows="4" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="interveneDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submittingIntervene" @click="confirmIntervene">确认干预</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="detailVisible" title="用户流失风险详情" width="700px" v-loading="detailLoading">
      <template v-if="currentUser">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="用户ID">{{ currentUser.user_id }}</el-descriptions-item>
          <el-descriptions-item label="风险等级">
            <el-tag :type="getRiskLevelType(currentUser.churn_risk)">{{ currentUser.churn_risk }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="最后活跃">{{ currentUser.last_activity_at }}</el-descriptions-item>
          <el-descriptions-item label="预测时间">{{ currentUser.predicted_at }}</el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">风险评分</el-divider>
        <div style="padding: 0 20px">
          <el-progress
            :percentage="Math.round(currentUser.churn_score || 0)"
            :color="getRiskColor(currentUser.churn_score)"
            :stroke-width="20"
          />
        </div>

        <el-divider content-position="left">流失原因分析</el-divider>
        <el-alert
          v-if="currentUser.risk_factors"
          :title="currentUser.risk_factors"
          type="warning"
          :closable="false"
          show-icon
        />

        <template v-if="currentUser.riskFactors && currentUser.riskFactors.length">
          <el-divider content-position="left">风险因素</el-divider>
          <el-table :data="currentUser.riskFactors" border size="small">
            <el-table-column prop="factor" label="因素" min-width="180" />
            <el-table-column prop="impact" label="影响程度" width="120">
              <template #default="{ row: r }">
                <el-tag :type="r.impact === '高' ? 'danger' : r.impact === '中' ? 'warning' : 'info'" size="small">
                  {{ r.impact }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="description" label="说明" min-width="200" show-overflow-tooltip />
          </el-table>
        </template>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onActivated } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Operation, Refresh } from '@element-plus/icons-vue'
import {
  getChurnPredictions,
  getChurnPrediction,
  getChurnWarnings,
  getChurnModelConfig,
  saveChurnModelConfig,
  runChurnPrediction,
  interveneUser,
  markWarningHandled,
} from '@/api/churnPrediction.js'
import { toList } from '@/utils/list.js'

const activeTab = ref('predictions')
const loadingPred = ref(false)
const loadingWarn = ref(false)
const predicting = ref(false)
const savingConfig = ref(false)
const submittingIntervene = ref(false)

const searchKeyword = ref('')
const users = ref([])
const warnings = ref([])
const stats = reactive({ highRisk: 0, mediumRisk: 0, lowRisk: 0, criticalRisk: 0 })

const configForm = reactive({
  id: 0,
  inactive_days_weight: 0.3,
  purchase_freq_weight: 0.3,
  order_value_weight: 0.2,
  engagement_weight: 0.2,
  inactive_threshold: 30,
  purchase_threshold: 60,
  high_risk_score: 70,
  critical_risk_score: 85,
})

const interveneDialogVisible = ref(false)
const interveneForm = reactive({ userId: 0, method: 'coupon', content: '' })
const detailVisible = ref(false)
const detailLoading = ref(false)
const currentUser = ref(null)

const filteredUsers = computed(() => {
  if (!searchKeyword.value) return users.value
  return users.value.filter((u) => (u.user_id || '').includes(searchKeyword.value))
})

const getRiskColor = (score) => {
  if (score >= 80) return '#EF4444'
  if (score >= 50) return '#F59E0B'
  return '#10B981'
}
const getRiskLevelType = (level) => {
  const map = { high: 'danger', critical: 'danger', medium: 'warning', low: 'success' }
  return map[(level || '').toLowerCase()] || 'info'
}
const warnLevelType = (level) => {
  const map = { high: 'danger', critical: 'danger', medium: 'warning', low: 'success' }
  return map[(level || '').toLowerCase()] || 'info'
}

const computeStats = (list) => {
  const counts = { high: 0, medium: 0, low: 0, critical: 0 }
  for (const u of list) {
    const level = (u.churn_risk || '').toLowerCase()
    if (level === 'high') counts.high++
    else if (level === 'critical') counts.critical++
    else if (level === 'medium') counts.medium++
    else if (level === 'low') counts.low++
  }
  stats.highRisk = counts.high
  stats.mediumRisk = counts.medium
  stats.lowRisk = counts.low
  stats.criticalRisk = counts.critical
}

const loadPredictions = async () => {
  loadingPred.value = true
  try {
    const res = await getChurnPredictions({ page: 1, page_size: 100 })
    users.value = toList(res?.data ?? res)
    computeStats(users.value)
  } catch (e) {
    ElMessage.error('加载预测失败：' + (e && e.message ? e.message : ''))
  } finally {
    loadingPred.value = false
  }
}

const loadWarnings = async () => {
  loadingWarn.value = true
  try {
    const res = await getChurnWarnings({ page: 1, page_size: 100 })
    warnings.value = toList(res?.data ?? res)
  } catch (e) {
    ElMessage.error('加载预警失败：' + (e && e.message ? e.message : ''))
  } finally {
    loadingWarn.value = false
  }
}

const loadConfig = async () => {
  try {
    const res = await getChurnModelConfig()
    const cfg = res?.data ?? res
    if (cfg && typeof cfg === 'object') {
      Object.assign(configForm, {
        id: cfg.id || 0,
        inactive_days_weight: cfg.inactive_days_weight ?? 0.3,
        purchase_freq_weight: cfg.purchase_freq_weight ?? 0.3,
        order_value_weight: cfg.order_value_weight ?? 0.2,
        engagement_weight: cfg.engagement_weight ?? 0.2,
        inactive_threshold: cfg.inactive_threshold ?? 30,
        purchase_threshold: cfg.purchase_threshold ?? 60,
        high_risk_score: cfg.high_risk_score ?? 70,
        critical_risk_score: cfg.critical_risk_score ?? 85,
      })
    }
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
}

const refreshAll = async () => {
  await Promise.all([loadPredictions(), loadWarnings(), loadConfig()])
}

const runPrediction = async () => {
  predicting.value = true
  try {
    await runChurnPrediction()
    ElMessage.success('预测完成')
    await refreshAll()
  } catch (e) {
    ElMessage.error('预测失败：' + (e && e.message ? e.message : ''))
  } finally {
    predicting.value = false
  }
}

const intervene = (row) => {
  interveneForm.userId = row.user_id
  interveneForm.method = 'coupon'
  interveneForm.content = ''
  interveneDialogVisible.value = true
}

const confirmIntervene = async () => {
  submittingIntervene.value = true
  try {
    await interveneUser({
      warning_id: interveneForm.userId,
      intervention_type: interveneForm.method,
      note: interveneForm.content,
    })
    ElMessage.success('干预已执行')
    interveneDialogVisible.value = false
  } catch (e) {
    ElMessage.error('干预失败：' + (e && e.message ? e.message : ''))
  } finally {
    submittingIntervene.value = false
  }
}

const viewUserDetail = async (row) => {
  detailVisible.value = true
  detailLoading.value = true
  try {
    const res = await getChurnPrediction({ user_id: row.user_id })
    let data = res?.data ?? res ?? {}
    if (typeof data.risk_factors === 'string' && data.risk_factors) {
      try {
        data.risk_factors = JSON.parse(data.risk_factors)
      } catch (e) {
        data.risk_factors = []
      }
    }
    currentUser.value = data
  } catch {
    currentUser.value = row
  } finally {
    detailLoading.value = false
  }
}

const handleWarning = async (row) => {
  try {
    const { value } = await ElMessageBox.prompt('请输入处理备注', '标记预警已处理', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      inputType: 'textarea',
    })
    await markWarningHandled(row.id, { note: value || '' })
    ElMessage.success('已标记为处理')
    await loadWarnings()
  } catch (e) {
    if (e !== 'cancel' && e && e.message) {
      ElMessage.error('操作失败：' + e.message)
    }
  }
}

const saveConfig = async () => {
  savingConfig.value = true
  try {
    await saveChurnModelConfig({ ...configForm })
    ElMessage.success('配置已保存')
  } catch (e) {
    ElMessage.error('保存失败：' + (e && e.message ? e.message : ''))
  } finally {
    savingConfig.value = false
  }
}

onMounted(() => refreshAll())
onActivated(() => refreshAll())
</script>

<style scoped lang="scss">
.churn-prediction-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.stats-row {
  margin-bottom: 20px;
  .stat-item {
    text-align: center;
    .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
    .stat-value { font-size: 28px; font-weight: bold; }
  }
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.handled-text { color: #909399; }
</style>
