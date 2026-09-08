<template>
  <div class="customer-event-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('客户事件追踪') }}</h2>
        <p class="subtitle">{{ $t('追踪客户在系统中的所有行为事件') }}</p>
      </div>
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>
        {{ $t('创建事件') }}
      </el-button>
    </el-card>

    <el-row :gutter="20" class="stats-row">
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('总事件数') }}</div>
            <div class="stat-value">{{ stats.total }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('事件类型') }}</div>
            <div class="stat-value">{{ stats.typeCount }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('事件来源') }}</div>
            <div class="stat-value">{{ stats.sourceCount }}</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <div class="stat-item">
            <div class="stat-label">{{ $t('事件总量') }}</div>
            <div class="stat-value">{{ stats.eventTypes }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('事件列表') }}</span>
          <div>
            <el-select v-model="filterType" :placeholder="$t('事件类型')" clearable style="width: 150px; margin-right: 10px">
              <el-option :label="$t('浏览')" value="view" />
              <el-option :label="$t('点击')" value="click" />
              <el-option :label="$t('注册')" value="register" />
              <el-option :label="$t('购买')" value="purchase" />
              <el-option :label="$t('分享')" value="share" />
            </el-select>
            <el-date-picker v-model="dateRange" type="daterange" range-separator="至" start-placeholder="开始" end-placeholder="结束" value-format="YYYY-MM-DD" style="width: 240px" />
          </div>
        </div>
      </template>
      <el-table :data="filteredEvents" v-loading="loading" stripe>
        <el-table-column prop="eventType" label="事件类型" width="120">
          <template #default="{ row }">
            <el-tag :type="getEventTypeTag(row.eventType)">{{ row.eventType }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="userId" label="用户ID" width="100" />
        <el-table-column prop="userName" label="用户" width="120" />
        <el-table-column prop="action" label="行为" min-width="150" />
        <el-table-column prop="target" label="目标对象" min-width="150" />
        <el-table-column prop="source" label="来源" width="100" />
        <el-table-column prop="ip" label="IP" width="130" />
        <el-table-column prop="createdAt" label="时间" width="180" />
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="viewEvent(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.size"
        :total="pagination.total"
        layout="total, prev, pager, next, jumper"
        @current-change="loadEvents"
        style="margin-top: 15px; text-align: right"
      />
    </el-card>

    <el-dialog v-model="dialogVisible" title="创建自定义事件" width="600px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="关联客户" required>
          <el-select v-model="form.customerId" filterable placeholder="选择客户" style="width: 100%">
            <el-option v-for="c in customerOptions" :key="c.id" :label="(c.name || c.id) + (c.phone ? ' (' + c.phone + ')' : '')" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="事件名称">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="事件类型">
          <el-select v-model="form.eventType" style="width: 100%">
            <el-option label="浏览" value="view" />
            <el-option label="点击" value="click" />
            <el-option label="自定义" value="custom" />
          </el-select>
        </el-form-item>
        <el-form-item label="触发条件">
          <el-input v-model="form.trigger" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitForm">创建</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="detailDialogVisible" title="事件详情" width="650px" :close-on-click-modal="false">
      <template v-if="currentEvent">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="事件类型">
            <el-tag :type="getEventTypeTag(currentEvent.eventType)">{{ currentEvent.eventType }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="行为">{{ currentEvent.action }}</el-descriptions-item>
          <el-descriptions-item label="用户ID">{{ currentEvent.userId }}</el-descriptions-item>
          <el-descriptions-item label="用户名">{{ currentEvent.userName }}</el-descriptions-item>
          <el-descriptions-item label="目标对象">{{ currentEvent.target }}</el-descriptions-item>
          <el-descriptions-item label="来源">{{ currentEvent.source }}</el-descriptions-item>
          <el-descriptions-item label="IP地址">{{ currentEvent.ip }}</el-descriptions-item>
          <el-descriptions-item label="时间">{{ currentEvent.createdAt }}</el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">事件数据 (JSON)</el-divider>
        <div class="event-data-json">
          <pre>{{ JSON.stringify(currentEvent, null, 2) }}</pre>
        </div>

        <template v-if="currentEvent.properties">
          <el-divider content-position="left">关联属性</el-divider>
          <el-descriptions :column="2" border>
            <el-descriptions-item
              v-for="(value, key) in currentEvent.properties"
              :key="key"
              :label="String(key)"
            >
              {{ value }}
            </el-descriptions-item>
          </el-descriptions>
        </template>
      </template>
      <template #footer>
        <el-button type="primary" @click="detailDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getCustomerEventHistory, getEventStats, trackEvent, getGlobalEvents } from '@/api/customerEvent.js'
import { getCustomerList } from '@/api/customer360.js'

const loading = ref(false)
const events = ref([])
const filterType = ref('')
const dateRange = ref([])
const dialogVisible = ref(false)
const form = ref({ name: '', eventType: 'view', trigger: '', userScope: 'all' })
const stats = ref({ total: 0, typeCount: 0, sourceCount: 0, eventTypes: 0 })
const pagination = ref({ page: 1, size: 20, total: 0 })
const detailDialogVisible = ref(false)
const currentEvent = ref(null)

const filteredEvents = computed(() => {
  let result = events.value
  if (filterType.value) result = result.filter(e => e.eventType === filterType.value)
  return result
})

const getEventTypeTag = (type) => {
  const map = { view: '', click: 'primary', register: 'success', purchase: 'warning', share: 'info' }
  return map[type]}

const mapEvent = (e) => {
  let data = {}
  try {
    data = typeof e.event_data === 'string' ? JSON.parse(e.event_data) : (e.event_data || {})
  } catch (_) {
    // JSON 解析失败时维持初始空对象
  }
  return {
    id: e.id,
    eventType: e.event_type || '',
    userId: e.customer_id || '',
    userName: data.user_name || data.name || e.customer_id || '',
    action: data.action || e.event_type || '',
    target: data.target || data.url || data.page || '',
    source: e.event_source || '',
    ip: data.ip || '',
    createdAt: (e.occurred_at || e.created_at || '').toString().replace('T', ' ').slice(0, 19),
    properties: data
  }
};

const loadEvents = async () => {
  loading.value = true
  try {
    const res = await getGlobalEvents({ page: pagination.value.page || 1, page_size: pagination.value.pageSize || 50 });
    const raw = Array.isArray(res) ? res : (res?.list || [])
    events.value = raw.map(mapEvent)
    pagination.value.total = res?.total ?? raw.length
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res = await getEventStats()
    const byType = res?.by_event_type || []
    const bySource = res?.by_event_source || []
    stats.value = {
      total: res?.total_events || 0,
      typeCount: byType.length,
      sourceCount: bySource.length,
      eventTypes: byType.reduce((s, x) => s + (x.count || 0), 0)
    }
  } catch (e) {
    console.error('加载统计失败', e)
  }
}

const customerOptions = ref([])
const loadCustomerOptions = async () => {
  try {
    const res = await getCustomerList({ pageSize: 200 })
    customerOptions.value = res?.list || []
  } catch (e) {
    console.error('加载客户选项失败', e)
  }
}

const showCreateDialog = () => {
  form.value = { customerId: '', name: '', eventType: 'view', trigger: '' }
  dialogVisible.value = true
  loadCustomerOptions()
}

const submitForm = async () => {
  if (!form.value.customerId) {
    ElMessage.warning('请选择关联客户')
    return
  }
  await trackEvent({
    customer_id: form.value.customerId,
    event_type: form.value.eventType,
    event_source: 'manual',
    event_data: { name: form.value.name, trigger: form.value.trigger }
  })
  ElMessage.success(i18n.global.t('事件创建成功'))
  dialogVisible.value = false
  loadEvents()
}

const viewEvent = (row) => {
  currentEvent.value = row
  detailDialogVisible.value = true
}

onMounted(() => {
  loadEvents()
  loadStats()
})
</script>

<style scoped lang="scss">
.customer-event-page { padding: 20px; }
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
.card-header { display: flex; justify-content: space-between; align-items: center; }
.event-data-json {
  background: #f5f7fa;
  border-radius: 4px;
  padding: 12px;
  max-height: 300px;
  overflow-y: auto;
}
.event-data-json pre {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
