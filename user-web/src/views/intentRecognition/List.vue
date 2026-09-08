<template>
  <div class="intent-recognition-page">
    
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>{{ $t('销售意图识别') }}</h2>
          <p class="subtitle">基于规则 + LLM 双引擎识别客户对话意图，支撑销冠 SOP 智能体决策</p>
        </div>
        <div class="header-actions">
          <el-tooltip
            :content="intentEnabled ? '关闭后业务链路将直接走 IntentUnknown 兜底，不再调用规则/LLM 识别' : '开启后业务链路进入规则 + LLM 识别流程'"
            placement="bottom"
            :show-after="200"
          >
            <el-switch
              v-model="intentEnabled"
              :loading="configLoading"
              inline-prompt
              active-text="启用"
              inactive-text="关闭"
              style="margin-right: 12px"
              @change="onToggleIntent"
            />
          </el-tooltip>
          <el-button @click="refreshAll" :loading="loading">
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
        </div>
      </div>
      <div v-if="configMeta.updated_at" class="config-meta">
        <el-tag size="small" effect="plain" type="info">
          最近更新：{{ formatTime(configMeta.updated_at) }} · by {{ configMeta.updated_by || 'system' }}
        </el-tag>
      </div>
    </el-card>

    
    <el-card shadow="never" class="test-card">
      <template #header>
        <div class="card-header">
          <span><el-icon><Aim /></el-icon> {{ $t('意图识别测试') }}</span>
          <el-button link type="primary" @click="fillExample">{{ $t('填入示例') }}</el-button>
        </div>
      </template>
      <el-form :model="testForm" label-width="90px">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="客户 ID">
              <el-input v-model="testForm.customer_id" placeholder="可选，用于客户画像归档" clearable />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="平台">
              <el-select v-model="testForm.platform" placeholder="可选，消息来源平台" clearable style="width: 100%">
                <el-option label="微信" value="wechat" />
                <el-option label="抖音" value="douyin" />
                <el-option label="小红书" value="xiaohongshu" />
                <el-option label="快手" value="kuaishou" />
                <el-option label="WhatsApp" value="whatsapp" />
                <el-option label="Telegram" value="telegram" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="上下文">
          <el-input v-model="testForm.context" placeholder="可选，前序对话上下文，帮助提升识别准确率" clearable />
        </el-form-item>
        <el-form-item label="客户消息">
          <el-input
            v-model="testForm.message"
            type="textarea"
            :rows="3"
            placeholder="输入客户的对话文本，如：这个多少钱？能便宜点吗？"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="recognizing" @click="runRecognize">
            <el-icon><Aim /></el-icon> 开始识别
          </el-button>
          <el-button type="success" :loading="batchLoading" @click="runBatchRecognize">批量识别(按行)</el-button>
        </el-form-item>
      </el-form>

      <template v-if="recognizeResult">
        <el-divider content-position="left">识别结果</el-divider>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="意图类型">
            <el-tag :type="getIntentTagType(recognizeResult.intent_type)">
              {{ recognizeResult.intent_name || intentNameMap[recognizeResult.intent_type] || recognizeResult.intent_type || '-' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="置信度">
            <el-progress
              :percentage="Math.round((recognizeResult.confidence || 0) * 100)"
              :color="getConfidenceColor(recognizeResult.confidence_level)"
              :stroke-width="14"
              :text-inside="true"
              style="width: 220px"
            />
          </el-descriptions-item>
          <el-descriptions-item label="置信级别">
            <el-tag :type="getConfidenceColor(recognizeResult.confidence_level)" size="small" effect="plain">
              {{ recognizeResult.confidence_level || '-' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="识别方法">
            <el-tag :type="recognizeResult.method === 'llm' ? 'danger' : 'info'" size="small" effect="plain">
              {{ recognizeResult.method === 'llm' ? 'LLM 模型' : '规则匹配' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="recognizeResult.platform" label="平台">
            {{ recognizeResult.platform }}
          </el-descriptions-item>
          <el-descriptions-item v-if="recognizeResult.latency_ms" label="耗时">
            {{ recognizeResult.latency_ms }} ms
          </el-descriptions-item>
        </el-descriptions>

        <template v-if="recognizeResult.entities && Object.keys(recognizeResult.entities).length">
          <el-divider content-position="left">提取实体</el-divider>
          <div class="entity-list">
            <el-tag v-for="(val, key) in recognizeResult.entities" :key="key" effect="plain" class="entity-tag">
              {{ key }}: {{ val }}
            </el-tag>
          </div>
        </template>
      </template>

      <template v-if="batchResults.length">
        <el-divider content-position="left">批量识别结果（{{ batchResults.length }} 条）</el-divider>
        <el-table :data="batchResults" stripe size="small" max-height="320">
          <el-table-column label="原文本" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">{{ row.message || row.raw_text }}</template>
          </el-table-column>
          <el-table-column label="意图" width="120">
            <template #default="{ row }">
              <el-tag :type="getIntentTagType(row.intent_type)" size="small">
                {{ row.intent_name || intentNameMap[row.intent_type] || row.intent_type }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="置信度" width="100">
            <template #default="{ row }">{{ Math.round((row.confidence || 0) * 100) }}%</template>
          </el-table-column>
          <el-table-column label="方法" width="80">
            <template #default="{ row }">
              <el-tag :type="getMethodTagType(row.method)" size="small" effect="plain">
                {{ getMethodLabel(row.method) }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-card>

    
    <el-row :gutter="16" class="stats-row" v-loading="statsLoading">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-value">{{ statsData.total || 0 }}</div>
          <div class="stat-label">总识别数</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-purchase">
          <div class="stat-value">{{ statsData.purchase || 0 }}</div>
          <div class="stat-label">购买意向</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-inquiry">
          <div class="stat-value">{{ statsData.price_inquiry || 0 }}</div>
          <div class="stat-label">价格咨询</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-objection">
          <div class="stat-value">{{ objectionTotal }}</div>
          <div class="stat-label">异议总量</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="16">
      
      <el-col :span="10">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>意图类型分布</span>
              <el-button link type="primary" size="small" @click="loadStats">刷新</el-button>
            </div>
          </template>
          <div class="intent-distribution" v-loading="statsLoading">
            <template v-if="distributionList.length">
              <div v-for="item in distributionList" :key="item.type" class="distribution-item">
                <span class="dist-name">
                  <el-tag :type="getIntentTagType(item.type)" size="small" effect="plain">
                    {{ intentNameMap[item.type] || item.type }}
                  </el-tag>
                </span>
                <div class="bar-track">
                  <div class="bar-fill" :style="{ width: distBarWidth(item.count) + '%', background: getIntentColor(item.type) }"></div>
                </div>
                <span class="dist-count">{{ item.count }}</span>
              </div>
            </template>
            <el-empty v-else description="暂无统计数据" :image-size="80" />
          </div>
        </el-card>
      </el-col>

      
      <el-col :span="14">
        <el-card shadow="never">
          <template #header>
            <div class="card-header">
              <span>最近识别记录</span>
              <div class="header-controls">
                <el-select v-model="filterIntentType" placeholder="意图类型筛选" clearable size="small" style="width: 160px" @change="onFilterChange">
                  <el-option v-for="d in intentDict" :key="d.type" :label="d.name" :value="d.type" />
                </el-select>
                <el-button size="small" type="primary" @click="loadRecent">查询</el-button>
              </div>
            </div>
          </template>
          <el-table :data="recentList" v-loading="recentLoading" stripe size="small" style="width: 100%">
            <el-table-column label="消息内容" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ row.message || row.raw_text }}</template>
            </el-table-column>
            <el-table-column label="识别意图" width="120">
              <template #default="{ row }">
                <el-tag :type="getIntentTagType(row.intent_type)" size="small">
                  {{ intentNameMap[row.intent_type] || row.intent_type }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="置信度" width="130">
              <template #default="{ row }">
                <el-progress
                  :percentage="Math.round((row.confidence || 0) * 100)"
                  :color="getConfidenceColor(row.confidence_level)"
                  :stroke-width="12"
                  :text-inside="true"
                />
              </template>
            </el-table-column>
            <el-table-column label="平台" width="90">
              <template #default="{ row }">
                <el-tag v-if="row.platform" size="small" effect="plain">{{ getChannelLabel(row.platform) }}</el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column label="时间" width="150">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <template #empty>
              <el-empty description="暂无记录" :image-size="80" />
            </template>
          </el-table>
          <div class="pagination-container" v-if="recentPagination.total > 0">
            <el-pagination
              v-model:current-page="recentPagination.page"
              v-model:page-size="recentPagination.pageSize"
              :page-sizes="[10, 20, 50]"
              layout="total, prev, pager, next"
              :total="recentPagination.total"
              @current-change="loadRecent"
              @size-change="loadRecent"
            />
          </div>
        </el-card>
      </el-col>
    </el-row>

    
    <el-card shadow="never" class="test-card">
      <template #header>
        <div class="card-header">
          <span><el-icon><MagicStick /></el-icon> 精细意图识别（8 大类 + 7 子类）</span>
          <el-button link type="primary" @click="fillFineExample">{{ $t('填入示例') }}</el-button>
        </div>
      </template>
      <el-form :model="fineForm" label-width="90px" inline>
        <el-form-item label="客户消息">
          <el-input v-model="fineForm.message" type="textarea" :rows="2" placeholder="输入客户消息，如：你们这个产品跟别家有什么区别？" style="width: 460px" />
        </el-form-item>
        <el-form-item label="客户 ID">
          <el-input v-model="fineForm.customer_id" placeholder="可选" clearable style="width: 180px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="fineLoading" @click="runRecognizeFine">
            <el-icon><MagicStick /></el-icon> 精细识别
          </el-button>
        </el-form-item>
      </el-form>
      <template v-if="fineResult">
        <el-divider content-position="left">精细识别结果</el-divider>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="大类">
            <el-tag type="primary" effect="dark">{{ fineResult.major || '-' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="子类">
            <el-tag effect="plain">{{ fineResult.minor || '-' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="置信度">
            <el-progress
              :percentage="Math.round((fineResult.confidence || 0) * 100)"
              :color="(fineResult.confidence || 0) >= 0.7 ? '#10B981' : '#F59E0B'"
              :stroke-width="14"
              style="width: 220px"
            />
          </el-descriptions-item>
          <el-descriptions-item label="方法">
            <el-tag :type="getFineMethodType(fineResult.method)" size="small">
              {{ getFineMethodLabel(fineResult.method) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="fineResult.llm_model" label="LLM 模型">
            <el-tag size="small" effect="plain">{{ fineResult.llm_model }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="fineResult.latency_ms" label="耗时">
            {{ fineResult.latency_ms }} ms
          </el-descriptions-item>
          <el-descriptions-item v-if="fineResult.reasoning" label="推理说明" :span="2">
            {{ fineResult.reasoning }}
          </el-descriptions-item>
        </el-descriptions>
      </template>
    </el-card>

    
    <el-card shadow="never" class="dict-card">
      <template #header>
        <div class="card-header">
          <span>精细意图识别日志（最近 {{ fineLogs.length }} 条）</span>
          <div class="header-controls">
            <el-select v-model="filterFineMajor" placeholder="大类筛选" clearable size="small" style="width: 160px" @change="loadFineLogs">
              <el-option v-for="m in fineMajorOptions" :key="m" :label="m" :value="m" />
            </el-select>
            <el-button size="small" type="primary" @click="loadFineLogs">查询</el-button>
          </div>
        </div>
      </template>
      <el-table :data="fineLogs" v-loading="fineLogsLoading" stripe size="small">
        <el-table-column label="消息内容" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ row.message || '-' }}</template>
        </el-table-column>
        <el-table-column label="大类" width="120">
          <template #default="{ row }">
            <el-tag type="primary" size="small" effect="plain">{{ row.intent_major || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="子类" width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ row.intent_minor || '-' }}</template>
        </el-table-column>
        <el-table-column label="置信度" width="100">
          <template #default="{ row }">{{ Math.round((row.confidence || 0) * 100) }}%</template>
        </el-table-column>
        <el-table-column label="方法" width="80">
          <template #default="{ row }">
            <el-tag :type="getFineMethodType(row.method)" size="small" effect="plain">
              {{ getFineMethodLabel(row.method) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="80">
          <template #default="{ row }">{{ row.latency_ms || 0 }}ms</template>
        </el-table-column>
        <el-table-column label="时间" width="150">
          <template #default="{ row }">{{ formatTime(row.timestamp || row.created_at) }}</template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无精细识别日志" :image-size="80" />
        </template>
      </el-table>
    </el-card>

    
    <el-card shadow="never" class="dict-card">
      <template #header>
        <div class="card-header">
          <span>意图词典（{{ intentDict.length }} 类）</span>
          <el-input v-model="dictKeyword" placeholder="搜索意图/关键词" clearable size="small" style="width: 220px" />
        </div>
      </template>
      <el-table :data="filteredDict" stripe size="small">
        <el-table-column label="意图" width="130">
          <template #default="{ row }">
            <el-tag :type="getIntentTagType(row.type)" size="small">{{ row.name }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="type" label="标识" width="180" />
        <el-table-column prop="description" label="说明" min-width="180" show-overflow-tooltip />
        <el-table-column label="关键词" min-width="260">
          <template #default="{ row }">
            <el-tag v-for="kw in (row.keywords || []).slice(0, 6)" :key="kw" size="small" effect="plain" style="margin: 2px">
              {{ kw }}
            </el-tag>
            <span v-if="(row.keywords || []).length > 6" class="more-tag">等 {{ row.keywords.length }} 个</span>
          </template>
        </el-table-column>
        <el-table-column label="示例" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            {{ (row.examples || []).join(' / ') }}
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Aim, MagicStick, Refresh } from '@element-plus/icons-vue'
import { intentApi } from '@/api/intentRecognition.js'
import { getChannelLabel } from '@/constants/channel'
const RECOGNIZE_METHOD = { llm: 'LLM', rule: '规则', keyword: '关键词', bert: 'BERT', hybrid: '混合' };
const RECOGNIZE_METHOD_TAG = { llm: 'danger', rule: 'info', keyword: 'success', bert: 'warning', hybrid: 'primary' }
const getMethodLabel = (m) => RECOGNIZE_METHOD[m] || m || '-'
const getMethodTagType = (m) => RECOGNIZE_METHOD_TAG[m] || ''

const statsLoading = ref(false);
const statsData = ref({})

const recentLoading = ref(false);
const recentList = ref([])
const filterIntentType = ref('')
const recentPagination = ref({ page: 1, pageSize: 20, total: 0 })

const intentDict = ref([]);
const dictKeyword = ref('')

const recognizing = ref(false);
const testForm = ref({ customer_id: '', platform: '', context: '', message: '' })
const recognizeResult = ref(null)

const batchLoading = ref(false);
const batchResults = ref([])

const fineLoading = ref(false);
const fineForm = ref({ message: '', customer_id: '' })
const fineResult = ref(null)
const fineLogsLoading = ref(false)
const fineLogs = ref([])
const filterFineMajor = ref('')
const fineMajorOptions = [
  'consult', 'price_inquiry', 'objection', 'after_sale',
  'complaint', 'churn', 'intent_buy', 'ask_product'
];
const FINE_METHOD_LABEL = { rule: '规则', llm: 'LLM', hybrid: '规则+LLM' }
const FINE_METHOD_TAG = { rule: 'success', llm: 'danger', hybrid: 'warning' }
const getFineMethodLabel = (m) => FINE_METHOD_LABEL[m] || m || '-'
const getFineMethodType = (m) => FINE_METHOD_TAG[m] || 'info'

const loading = ref(false)

const configLoading = ref(false);
const intentEnabled = ref(true)
const configMeta = ref({ updated_at: '', updated_by: '' })

const loadIntentConfig = async () => {
  configLoading.value = true
  try {
    const res = await intentApi.getConfig()
    if (res && typeof res === 'object') {
      intentEnabled.value = res.enabled !== false
      configMeta.value = {
        updated_at: res.updated_at || '',
        updated_by: res.updated_by || ''
      }
    }
  } catch (e) {
    intentEnabled.value = true;
    ElMessage.warning('加载意图识别配置失败，已使用默认开启状态：' + (e.message || '未知错误'))
  } finally {
    configLoading.value = false
  }
};

const onToggleIntent = async (val) => {
  configLoading.value = true
  try {
    const res = await intentApi.updateConfig({ enabled: val })
    if (res && typeof res === 'object') {
      configMeta.value = {
        updated_at: res.updated_at || '',
        updated_by: res.updated_by || ''
      }
    }
    ElMessage.success(`意图识别已${val ? '启用' : '关闭'}，业务链路立即生效`)
  } catch (e) {
    intentEnabled.value = !val;
    ElMessage.error('更新意图识别配置失败：' + (e.message || '未知错误'))
  } finally {
    configLoading.value = false
  }
};

const intentNameMap = computed(() => {
  const map = {}
  intentDict.value.forEach(d => { map[d.type] = d.name })
  return map
});

const objectionTotal = computed(() => {
  const s = statsData.value
  return (s.objection_price || 0) + (s.objection_need || 0) + (s.objection_trust || 0) +
    (s.objection_competitor || 0) + (s.objection_timing || 0)
})

const distributionList = computed(() => {
  const s = statsData.value
  if (Array.isArray(s.distribution)) {
    return s.distribution
  }
  return Object.entries(s)
    .filter(([k]) => !['total', 'distribution'].includes(k))
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count)
})

const filteredDict = computed(() => {
  if (!dictKeyword.value) return intentDict.value
  const kw = dictKeyword.value.toLowerCase()
  return intentDict.value.filter(d =>
    d.name.includes(kw) || d.type.includes(kw) ||
    (d.keywords || []).some(k => k.includes(kw))
  )
})

const distBarWidth = (count) => {
  const max = Math.max(...distributionList.value.map(d => d.count), 1)
  return Math.round((count / max) * 100)
}

const getIntentTagType = (type) => {
  if (!type) return 'info'
  if (type === 'purchase') return 'success'
  if (type === 'price_inquiry' || type === 'ask_product') return 'primary'
  if (type.startsWith('objection')) return 'warning'
  if (type === 'churn' || type === 'complaint') return 'danger'
  if (type === 'unknown') return 'info'
  return 'info'
}

const getIntentColor = (type) => {
  const map = {
    purchase: '#10B981',
    price_inquiry: '#4F46E5',
    ask_product: '#4F46E5',
    ask_service: '#909399',
    after_sale: '#F59E0B',
    objection_price: '#F59E0B',
    objection_need: '#F59E0B',
    objection_trust: '#F59E0B',
    objection_competitor: '#F59E0B',
    objection_timing: '#F59E0B',
    churn: '#EF4444',
    complaint: '#EF4444',
    social: '#909399',
    greeting: '#909399',
    unknown: '#c0c4cc'
  }
  return map[type] || '#909399'
}

const getConfidenceColor = (level) => {
  if (level === 'high') return '#10B981'
  if (level === 'medium') return '#F59E0B'
  return '#EF4444'
}

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const loadStats = async () => {
  statsLoading.value = true
  try {
    const res = await intentApi.getStats({
      page: 1,
      page_size: 100
    })
    if (res && typeof res === 'object') {
      statsData.value = res
    } else {
      statsData.value = {}
    }
  } catch (e) {
    ElMessage.error('加载统计失败：' + (e.message || '未知错误'))
  } finally {
    statsLoading.value = false
  }
}

const onFilterChange = () => {
  recentPagination.value.page = 1
  loadRecent()
}

const loadRecent = async () => {
  recentLoading.value = true
  try {
    const params = {
      page: recentPagination.value.page,
      page_size: recentPagination.value.pageSize
    }
    if (filterIntentType.value) params.intent_type = filterIntentType.value
    const res = await intentApi.getRecent(params)
    recentList.value = res?.list || (Array.isArray(res) ? res : []);
    recentPagination.value.total = res?.total || recentList.value.length
  } catch (e) {
    ElMessage.error('加载最近记录失败：' + (e.message || '未知错误'))
  } finally {
    recentLoading.value = false
  }
}

const loadDict = async () => {
  try {
    const res = await intentApi.getDict()
    intentDict.value = Array.isArray(res) ? res : (res?.list || [])
  } catch (e) {
    ElMessage.error('加载意图词典失败：' + (e.message || '未知错误'))
  }
}

const refreshAll = async () => {
  loading.value = true
  try {
    await Promise.all([loadStats(), loadRecent(), loadDict()])
  } finally {
    loading.value = false
  }
}

const fillExample = () => {
  const examples = [
    '这个多少钱？能便宜点吗？',
    '太贵了，不值这个价',
    '我要买，怎么付款？',
    '别家比你们便宜多了',
    '暂时不需要，再看看吧'
  ]
  testForm.value.message = examples[Math.floor(Math.random() * examples.length)]
}

const runRecognize = async () => {
  if (!testForm.value.message || !testForm.value.message.trim()) {
    ElMessage.warning(i18n.global.t('请输入客户消息'))
    return
  }
  recognizing.value = true
  batchResults.value = []
  try {
    const data = {
      message: testForm.value.message
    }
    if (testForm.value.context) data.context = testForm.value.context
    if (testForm.value.customer_id) data.customer_id = testForm.value.customer_id
    if (testForm.value.platform) data.platform = testForm.value.platform
    const res = await intentApi.recognize(data)
    recognizeResult.value = res
    ElMessage.success(i18n.global.t('识别完成'))
    loadRecent()
  } catch (e) {
    ElMessage.error('识别失败：' + (e.message || '未知错误'))
  } finally {
    recognizing.value = false
  }
}

const runBatchRecognize = async () => {
  const lines = (testForm.value.message || '').split('\n').map(s => s.trim()).filter(Boolean);
  if (lines.length === 0) {
    ElMessage.warning(i18n.global.t('请在客户消息框输入多行文本，每行一条进行批量识别'))
    return
  }
  batchLoading.value = true
  recognizeResult.value = null
  batchResults.value = []
  try {
    const res = await intentApi.batchRecognize({ messages: lines })
    const list = Array.isArray(res) ? res : (res?.list || [])
    batchResults.value = list.map((r, i) => ({ ...r, message: r.message || lines[i] }))
    ElMessage.success(`批量识别完成，共 ${batchResults.value.length} 条`)
    loadRecent()
  } catch (e) {
    ElMessage.error('批量识别失败：' + (e.message || '未知错误'))
  } finally {
    batchLoading.value = false
  }
}

const fillFineExample = () => {
  const examples = [
    '你们这个产品跟别家有什么区别？',
    '太贵了，能便宜点吗？',
    '我要怎么付款？',
    '我刚买的那个有质量问题，要求退款',
    '别家只要 299，你们太贵了',
    '我想确认下具体功能再决定'
  ]
  fineForm.value.message = examples[Math.floor(Math.random() * examples.length)]
};

const runRecognizeFine = async () => {
  if (!fineForm.value.message || !fineForm.value.message.trim()) {
    ElMessage.warning(i18n.global.t('请输入客户消息'))
    return
  }
  fineLoading.value = true
  fineResult.value = null
  try {
    const data = { message: fineForm.value.message }
    if (fineForm.value.customer_id) data.customer_id = fineForm.value.customer_id
    const res = await intentApi.recognizeFine(data)
    fineResult.value = res
    ElMessage.success(i18n.global.t('精细识别完成'))
    loadFineLogs()
  } catch (e) {
    ElMessage.error('精细识别失败：' + (e.message || '未知错误'))
  } finally {
    fineLoading.value = false
  }
}

const loadFineLogs = async () => {
  fineLogsLoading.value = true
  try {
    const params = { limit: 50 }
    if (filterFineMajor.value) params.major = filterFineMajor.value
    const res = await intentApi.getLogs(params)
    fineLogs.value = Array.isArray(res) ? res : (res?.list || [])
  } catch (e) {
    ElMessage.error('加载精细识别日志失败：' + (e.message || '未知错误'))
    fineLogs.value = []
  } finally {
    fineLogsLoading.value = false
  }
}

onMounted(() => {
  loadIntentConfig()
  refreshAll()
  loadFineLogs()
})
</script>

<style scoped lang="scss">
.intent-recognition-page { padding: 20px; }

.header-card {
  margin-bottom: 16px;
  :deep(.el-card__body) { padding: 16px 20px; }
  .header-content {
    display: flex;
    justify-content: space-between;
    align-items: center;
    h2 { margin: 0 0 6px 0; font-size: 20px; }
    .subtitle { color: #909399; margin: 0; font-size: 13px; }
  }
  .header-actions { display: flex; gap: 8px; }
}
.config-meta { margin-top: 12px; padding-top: 12px; border-top: 1px dashed #ebeef5; }

.test-card { margin-bottom: 16px; }

.stats-row {
  margin-bottom: 16px;
  .stat-card {
    text-align: center;
    .stat-value { font-size: 28px; font-weight: bold; color: #303133; }
    .stat-label { color: #909399; font-size: 13px; margin-top: 6px; }
    &.stat-purchase .stat-value { color: #10B981; }
    &.stat-inquiry .stat-value { color: #4F46E5; }
    &.stat-objection .stat-value { color: #F59E0B; }
  }
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  .header-controls { display: flex; gap: 8px; }
}

.intent-distribution {
  min-height: 200px;
  .distribution-item {
    display: flex;
    align-items: center;
    margin-bottom: 12px;
    .dist-name { width: 110px; flex-shrink: 0; }
    .bar-track {
      flex: 1;
      height: 14px;
      background: #f0f2f5;
      border-radius: 7px;
      overflow: hidden;
      margin: 0 10px;
      .bar-fill { height: 100%; border-radius: 7px; transition: width .3s; }
    }
    .dist-count { width: 40px; text-align: right; font-weight: bold; color: #606266; }
  }
}

.dict-card { margin-top: 16px; }
.more-tag { color: #909399; font-size: 12px; margin-left: 4px; }

.entity-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  .entity-tag { margin: 2px; }
}

.pagination-container {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}
</style>
