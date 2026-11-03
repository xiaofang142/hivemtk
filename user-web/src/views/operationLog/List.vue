<template>
  <div class="operation-log-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('操作日志') }}</h2>
        <p class="subtitle">
          {{ $t('审计追踪所有用户操作记录') }}
        </p>
      </div>
      <div>
        <el-button @click="$router.push('/operationLog/enhanced')">
          增强视图
        </el-button>
        <el-button @click="exportLogs">
          <el-icon><Download /></el-icon>
          {{ $t('导出') }}
        </el-button>
        <el-button @click="refreshData">
          <el-icon><Refresh /></el-icon>
          {{ $t('刷新') }}
        </el-button>
      </div>
    </el-card>

    <el-card>
      <div class="filter-bar">
        <el-input
          v-model="searchKeyword"
          :placeholder="$t('搜索操作内容')"
          clearable
          style="width: 200px"
        />
        <el-select
          v-model="filterModule"
          :placeholder="$t('模块')"
          clearable
          style="width: 150px"
        >
          <el-option
            :label="$t('用户')"
            value="user"
          />
          <el-option
            :label="$t('线索')"
            value="clue"
          />
          <el-option
            :label="$t('营销')"
            value="marketing"
          />
          <el-option
            :label="$t('系统')"
            value="system"
          />
        </el-select>
        <el-select
          v-model="filterAction"
          :placeholder="$t('操作类型')"
          clearable
          style="width: 150px"
        >
          <el-option
            :label="$t('创建')"
            value="create"
          />
          <el-option
            :label="$t('更新')"
            value="update"
          />
          <el-option
            :label="$t('删除')"
            value="delete"
          />
          <el-option
            :label="$t('登录')"
            value="login"
          />
          <el-option
            :label="$t('登出')"
            value="logout"
          />
        </el-select>
        <el-select
          v-model="filterUser"
          :placeholder="$t('操作人')"
          clearable
          style="width: 150px"
        >
          <el-option
            label="admin"
            value="admin"
          />
          <el-option
            label="manager"
            value="manager"
          />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="至"
          start-placeholder="开始"
          end-placeholder="结束"
          value-format="YYYY-MM-DD"
        />
      </div>

      <el-table
        v-loading="loading"
        :data="filteredLogs"
        stripe
      >
        <el-table-column
          prop="id"
          label="ID"
          width="80"
        />
        <el-table-column
          prop="username"
          :label="$t('操作人')"
          width="100"
        />
        <el-table-column
          prop="module"
          :label="$t('模块')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag size="small">
              {{ row.module }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="action"
          label="操作"
          width="100"
        >
          <template #default="{ row }">
            <el-tag
              :type="getActionType(row.action)"
              size="small"
            >
              {{ getActionText(row.action) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="resource"
          label="目标对象"
          min-width="150"
        >
          <template #default="{ row }">
            <span v-if="row.resource_id">{{ row.resource }} #{{ row.resource_id }}</span>
            <span v-else>{{ row.resource }}</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="detail"
          label="操作描述"
          min-width="250"
          show-overflow-tooltip
        />
        <el-table-column
          prop="ip"
          label="IP地址"
          width="130"
        />
        <el-table-column
          prop="user_agent"
          label="浏览器"
          min-width="200"
          show-overflow-tooltip
        />
        <el-table-column
          prop="created_at"
          label="时间"
          width="180">
          <template #default="{ row }">
            {{ fmtTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="100"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="viewDetail(row)"
            >
              详情
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.size"
        :total="pagination.total"
        layout="total, prev, pager, next, jumper"
        style="margin-top: 15px; text-align: right"
        @current-change="loadLogs"
      />
    </el-card>

    <el-dialog
      v-model="detailVisible"
      title="日志详情"
      width="700px"
    >
      <el-descriptions
        v-if="currentLog"
        :column="1"
        border
      >
        <el-descriptions-item label="操作人">
          {{ currentLog.username }}
        </el-descriptions-item>
        <el-descriptions-item label="模块">
          {{ currentLog.module }}
        </el-descriptions-item>
        <el-descriptions-item label="操作类型">
          {{ getActionText(currentLog.action) }}
        </el-descriptions-item>
        <el-descriptions-item label="目标对象">
          <span v-if="currentLog.resource_id">{{ currentLog.resource }} #{{ currentLog.resource_id }}</span>
          <span v-else>{{ currentLog.resource }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="操作描述">
          {{ currentLog.detail }}
        </el-descriptions-item>
        <el-descriptions-item label="修改前">
          <pre>{{ currentLog.old_value }}</pre>
        </el-descriptions-item>
        <el-descriptions-item label="修改后">
          <pre>{{ currentLog.new_value }}</pre>
        </el-descriptions-item>
        <el-descriptions-item label="IP地址">
          {{ currentLog.ip }}
        </el-descriptions-item>
        <el-descriptions-item label="浏览器">
          {{ currentLog.user_agent }}
        </el-descriptions-item>
        <el-descriptions-item label="时间">
          {{ fmtTime(currentLog.created_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Download, Refresh } from '@element-plus/icons-vue'
import { getOperationLogs, exportOperationLogs, getOperationLogDetail } from '@/api/operationLog.js'
import { fmtTime } from '@/utils/time'
import { isUserReportedError } from '@/utils/errorReporting'

const loading = ref(false)
const searchKeyword = ref('')
const filterModule = ref('')
const filterAction = ref('')
const filterUser = ref('')
const dateRange = ref([])
const logs = ref([])
const pagination = ref({ page: 1, size: 20, total: 0 })
const detailVisible = ref(false)
const currentLog = ref(null)

const filteredLogs = computed(() => {
  let result = logs.value
  if (searchKeyword.value) result = result.filter(l => l.description?.includes(searchKeyword.value))
  if (filterModule.value) result = result.filter(l => l.module === filterModule.value)
  if (filterAction.value) result = result.filter(l => l.action === filterAction.value)
  if (filterUser.value) result = result.filter(l => l.username === filterUser.value)
  return result
})

const getActionType = (action) => {
  const map = { create: 'success', update: 'primary', delete: 'danger', login: 'warning', logout: 'info' }
  return map[action]}
const getActionText = (action) => {
  const map = { create: '创建', update: '更新', delete: '删除', login: '登录', logout: '登出' }
  return map[action] || action
}

const loadLogs = async () => {
  loading.value = true
  try {
    const res = await getOperationLogs({ page: pagination.value.page, size: pagination.value.size })
    logs.value = res?.list || [];
    pagination.value.total = res?.total || 0
  } catch (e) {
    if (!isUserReportedError(e)) ElMessage.error('加载操作日志失败')
    logs.value = []
  } finally {
    loading.value = false
  }
}

const refreshData = () => loadLogs()

const exportLogs = async () => {
  try {
    const params= {}
    if (filterModule.value) params.module = filterModule.value
    if (filterAction.value) params.action = filterAction.value
    if (dateRange.value && dateRange.value.length === 2) {
      params.start_date = dateRange.value[0]
      params.end_date = dateRange.value[1]
    }
    const blob = await exportOperationLogs(params)
    const url = window.URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `operation-logs-${new Date().toISOString().slice(0, 10)}.csv`
    document.body.appendChild(anchor)
    anchor.click()
    document.body.removeChild(anchor)
    window.URL.revokeObjectURL(url)
    ElMessage.success(i18n.global.t('导出成功'))
  } catch (error) {
    ElMessage.error('导出失败: ' + (error.message))
  }
}

const viewDetail = async (row) => {
  const res = await getOperationLogDetail(row.id)
  currentLog.value = res
  detailVisible.value = true
}

onMounted(() => loadLogs())
</script>

<style scoped lang="scss">
.operation-log-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) { display: flex; justify-content: space-between; align-items: center; }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
}
.filter-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 15px;
  flex-wrap: wrap;
}
</style>
