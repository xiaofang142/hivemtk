<template>
  <div class="backup-page">
    <el-row
      :gutter="16"
      class="stats-row"
    >
      <el-col :span="6">
        <el-card class="stat-card">
          <div class="stat-label">
            总备份数
          </div>
          <div class="stat-value">
            {{ stats.total }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card success">
          <div class="stat-label">
            最近成功
          </div>
          <div class="stat-value">
            {{ stats.lastSuccess }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card warning">
          <div class="stat-label">
            总大小
          </div>
          <div class="stat-value">
            {{ formatSize(stats.totalSize) }}
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card primary">
          <div class="stat-label">
            下次计划
          </div>
          <div
            class="stat-value"
            style="font-size: 16px"
          >
            {{ stats.nextRun }}
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>备份策略（USR-SY-02）</span>
          <el-button
            type="primary"
            @click="saveStrategy"
          >
            保存策略
          </el-button>
        </div>
      </template>
      <el-form
        :model="strategy"
        label-width="120px"
      >
        <el-form-item label="自动备份">
          <el-switch v-model="strategy.enabled" />
        </el-form-item>
        <el-form-item label="每日">
          <el-time-picker
            v-model="strategy.daily"
            placeholder="02:00"
          />
        </el-form-item>
        <el-form-item label="每周">
          <el-select
            v-model="strategy.weeklyDay"
            style="width: 120px"
          >
            <el-option
              label="周一"
              :value="1"
            />
            <el-option
              label="周日"
              :value="0"
            />
          </el-select>
          <el-time-picker
            v-model="strategy.weeklyTime"
            placeholder="03:00"
            style="margin-left: 8px"
          />
        </el-form-item>
        <el-form-item label="保留天数">
          <el-input-number
            v-model="strategy.retentionDays"
            :min="7"
            :max="365"
          />
        </el-form-item>
        <el-form-item label="校验和">
          <el-switch v-model="strategy.checksum" />
        </el-form-item>
      </el-form>
    </el-card>

    <el-card class="restore-card">
      <template #header>
        <div class="card-header">
          <span>备份列表</span>
          <el-button
            type="danger"
            :loading="backing"
            @click="createBackup"
          >
            立即备份
          </el-button>
        </div>
      </template>
      <el-table
        v-loading="loading"
        :data="backups"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="100"
        />
        <el-table-column
          prop="createdAt"
          label="备份时间"
          width="170">
          <template #default="{ row }">
            {{ fmtTime(row.createdAt) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="size"
          label="大小"
          width="100"
        >
          <template #default="{ row }">
            {{ formatSize(row.size) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="checksum"
          label="SHA256"
          width="160"
        />
        <el-table-column
          prop="status"
          label="状态"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.status === 'ok' ? 'success' : 'danger'">
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="200"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="showPreview(row)"
            >
              预览影响
            </el-button>
            <el-button
              link
              type="warning"
              @click="restore(row)"
            >
              恢复
            </el-button>
            <el-button
              link
              type="danger"
              @click="remove(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="previewVisible"
      title="恢复影响预览"
      width="600px"
    >
      <el-alert
        type="warning"
        :closable="false"
      >
        即将恢复此备份：会影响以下数据（行数估算）
      </el-alert>
      <el-table :data="previewData">
        <el-table-column
          prop="table"
          label="表名"
        />
        <el-table-column
          prop="rows"
          label="行数估算"
          width="160"
        />
      </el-table>
      <template #footer>
        <el-button @click="previewVisible = false">
          取消
        </el-button>
        <el-button
          type="danger"
          @click="confirmRestore"
        >
          确认恢复
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import { formatFileSize } from '@/utils/format'
import { http } from '@/utils/request'
import { fmtTime } from '@/utils/time'

const stats = ref({ total: 0, lastSuccess: '-', totalSize: 0, nextRun: '-' })
const strategy = reactive({
  enabled: true,
  daily: new Date(2024, 0, 1, 2, 0),
  weeklyDay: 0,
  weeklyTime: new Date(2024, 0, 1, 3, 0),
  retentionDays: 30,
  checksum: true
})
const backups = ref([])
const loading = ref(false)
const backing = ref(false)
const previewVisible = ref(false)
const previewData = ref([])
let _selectedBackup = null

const formatSize = formatFileSize

async function load() {
  loading.value = true
  try {
    const [s, b] = await Promise.all([
      http.get('/api/backup/stats'),
      http.get('/api/backup/list')
    ])
    stats.value = s || stats.value
    backups.value = b || []
  } finally {
    loading.value = false
  }
}

async function saveStrategy() {
  const d = strategy.daily instanceof Date ? strategy.daily : new Date(strategy.daily);
  const w = strategy.weeklyTime instanceof Date ? strategy.weeklyTime : new Date(strategy.weeklyTime)
  await http.put('/api/backup/strategy', {
    enabled: strategy.enabled,
    daily_hour: d.getHours(),
    daily_minute: d.getMinutes(),
    weekly_day: strategy.weeklyDay,
    weekly_hour: w.getHours(),
    weekly_minute: w.getMinutes(),
    retention_days: strategy.retentionDays,
    checksum: strategy.checksum
  })
  ElMessage.success('策略已保存')
}

async function createBackup() {
  backing.value = true
  try {
    await http.post('/api/backup/create', {})
    ElMessage.success('备份中...')
    setTimeout(load, 3000)
  } finally {
    backing.value = false
  }
}

async function showPreview(row) {
  _selectedBackup = row
  previewData.value = await http.get(`/api/backup/${row.id}/preview`)
  previewVisible.value = true
}

async function confirmRestore() {
  if (!_selectedBackup) return
  try {
    await ElMessageBox.confirm('确认恢复？此操作不可逆', '危险操作', { type: 'error' })
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    throw e
  }
  await http.post(`/api/backup/${_selectedBackup.id}/restore`, {})
  ElMessage.success('恢复指令已下发')
  previewVisible.value = false
}

function restore(row) {
  _selectedBackup = row
  showPreview(row)
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除备份 ${row.id}？`, '删除确认', { type: 'warning' })
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    throw e
  }
  await http.delete(`/api/backup/${row.id}`)
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>

<style scoped>
.backup-page { padding: 16px; }
.stats-row { margin-bottom: 16px; }
.stat-card { text-align: center; padding: 8px; }
.stat-label { color: #64748B; font-size: 12px; }
.stat-value { font-size: 28px; font-weight: 700; margin: 8px 0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.restore-card { margin-top: 16px; }
</style>
