<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>渠道能力运行诊断</span>
          <el-button
            size="small"
            :loading="loading"
            @click="load"
          >
            刷新
          </el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        class="semantics-alert"
      >
        <template #title>
          三层判定口径：注册表声明 ≠ 本二进制实现
        </template>
        <div class="semantics-body">
          <p>
            <strong>implemented</strong>：本二进制里存在该「平台 × 能力」的真实执行路径。能力注册表（capregistry）登记了能力，只代表它被声明，不代表这里为 true。
          </p>
          <p>
            <strong>configured</strong>：该平台已配置可用账号/连接器。实现存在但未配置时，是 configured=false，而不是 implemented=false —— 两者的修复动作不同。
          </p>
          <p>
            <strong>ready = implemented && configured</strong>；未就绪时 reason_code 给出机器可读原因（<code>no_runtime_path</code> / <code>not_configured_no_account</code>）。
          </p>
        </div>
      </el-alert>

      <el-alert
        v-if="error"
        type="error"
        :closable="false"
        show-icon
        :title="error"
        class="error-alert"
      />

      <el-row
        :gutter="16"
        class="summary-row"
      >
        <el-col
          :xs="24"
          :sm="8"
        >
          <div class="summary-item">
            <div class="summary-label">
              已就绪 / 已登记
            </div>
            <div class="summary-value">
              {{ readyCount }} <span class="summary-divider">/</span> {{ total }}
            </div>
          </div>
        </el-col>
        <el-col
          :xs="24"
          :sm="8"
        >
          <div class="summary-item">
            <div class="summary-label">
              就绪率
            </div>
            <div class="summary-value">
              {{ readyPercent }}%
            </div>
          </div>
        </el-col>
        <el-col
          :xs="24"
          :sm="8"
        >
          <div class="summary-item summary-progress">
            <el-progress
              :percentage="readyPercent"
              :status="readyPercent === 100 ? 'success' : undefined"
            />
          </div>
        </el-col>
      </el-row>

      <div class="toolbar">
        <el-select
          v-model="filters.platform"
          placeholder="平台"
          clearable
          style="width: 160px"
        >
          <el-option
            v-for="p in platformOptions"
            :key="p"
            :label="p"
            :value="p"
          />
        </el-select>
        <el-select
          v-model="filters.reasonCode"
          placeholder="原因码"
          clearable
          style="width: 220px; margin-left: 8px"
        >
          <el-option
            v-for="r in reasonOptions"
            :key="r"
            :label="reasonText(r) + '（' + r + '）'"
            :value="r"
          />
        </el-select>
        <el-checkbox
          v-model="filters.onlyNotReady"
          class="not-ready-check"
        >
          仅看未就绪
        </el-checkbox>
        <el-button
          class="reset-btn"
          @click="resetFilters"
        >
          重置
        </el-button>
        <span class="filter-count">当前显示 {{ filteredRows.length }} 条</span>
      </div>

      <el-table
        v-loading="loading"
        :data="filteredRows"
        border
        size="small"
        :row-class-name="rowClassName"
      >
        <el-table-column
          prop="platform"
          label="平台"
          width="140"
        />
        <el-table-column
          prop="capability"
          label="能力"
          width="150"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ row.capability }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="via"
          label="接入方式"
          width="140"
        />
        <el-table-column
          label="已实现"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.implemented ? 'success' : 'danger'"
              size="small"
              effect="plain"
            >
              {{ row.implemented ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="已配置"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.configured ? 'success' : 'info'"
              size="small"
              effect="plain"
            >
              {{ row.configured ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="就绪"
          width="90"
          align="center"
        >
          <template #default="{ row }">
            <el-tag
              :type="row.ready ? 'success' : 'warning'"
              size="small"
            >
              {{ row.ready ? '就绪' : '未就绪' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="原因码"
          width="230"
        >
          <template #default="{ row }">
            <template v-if="row.reason_code">
              <span>{{ reasonText(row.reason_code) }}</span>
              <span class="muted">（{{ row.reason_code }}）</span>
            </template>
            <span
              v-else
              class="muted"
            >—</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="note"
          label="说明"
          min-width="240"
          show-overflow-tooltip
        />
        <template #empty>
          <el-empty description="没有符合条件的能力登记" />
        </template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { capabilityDiagnosticsApi } from '@/api/capabilityDiagnostics'

const rows = ref([])
const total = ref(0)
const readyCount = ref(0)
const loading = ref(false)
const error = ref('')

const filters = reactive({
  platform: '',
  reasonCode: '',
  onlyNotReady: false
})

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const res = await capabilityDiagnosticsApi.get()
    // 拦截器已拆信封：正常拿到 { capabilities, total, ready_count }；
    // 这里仍兼容 res.data 形态，避免网关/代理层包装差异让页面空白。
    const data = res && Array.isArray(res.capabilities) ? res : (res?.data ?? {})
    const list = Array.isArray(data.capabilities) ? data.capabilities : []
    rows.value = list
    total.value = Number(data.total ?? list.length)
    readyCount.value = Number(data.ready_count ?? list.filter((r) => r.ready).length)
  } catch (e) {
    error.value = e.message || '加载能力诊断失败'
    rows.value = []
    total.value = 0
    readyCount.value = 0
  } finally {
    loading.value = false
  }
}

const platformOptions = computed(() => {
  const set = new Set(rows.value.map((r) => r.platform).filter(Boolean))
  return [...set].sort()
})

const reasonOptions = computed(() => {
  const set = new Set(rows.value.map((r) => r.reason_code).filter(Boolean))
  return [...set].sort()
})

const readyPercent = computed(() => {
  if (!total.value) return 0
  return Math.round((readyCount.value / total.value) * 100)
})

const filteredRows = computed(() =>
  rows.value.filter((row) => {
    if (filters.platform && row.platform !== filters.platform) return false
    if (filters.onlyNotReady && row.ready) return false
    if (filters.reasonCode && row.reason_code !== filters.reasonCode) return false
    return true
  })
)

const resetFilters = () => {
  filters.platform = ''
  filters.reasonCode = ''
  filters.onlyNotReady = false
}

const reasonText = (code) => {
  const map = {
    no_runtime_path: '无运行时执行路径',
    not_configured_no_account: '未配置可用账号/连接器'
  }
  return map[code] || code
}

const rowClassName = ({ row }) => (row.ready ? '' : 'row-not-ready')

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.semantics-alert {
  margin-bottom: 12px;
}

.semantics-body p {
  margin: 4px 0;
  line-height: 1.6;
}

.semantics-body code {
  padding: 1px 4px;
  border-radius: 3px;
  background: #f5f7fa;
  font-size: 12px;
}

.error-alert {
  margin-bottom: 12px;
}

.summary-row {
  margin-bottom: 12px;
}

.summary-item {
  padding: 12px 16px;
  border-radius: 6px;
  background: #f5f7fa;
}

.summary-progress {
  display: flex;
  align-items: center;
}

.summary-label {
  font-size: 13px;
  color: #909399;
  margin-bottom: 6px;
}

.summary-value {
  font-size: 24px;
  font-weight: 600;
  color: #303133;
}

.summary-divider {
  color: #c0c4cc;
  font-weight: 400;
}

.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.not-ready-check {
  margin-left: 8px;
}

.reset-btn {
  margin-left: 8px;
}

.filter-count {
  margin-left: auto;
  color: #909399;
  font-size: 13px;
}

.muted {
  color: #909399;
}

:deep(.el-table__row.row-not-ready) td {
  background: #fef0f0;
}
</style>
