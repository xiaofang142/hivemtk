<template>
  <div class="notifications-page">
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>{{ $t('通知中心') }}</h2>
          <p class="subtitle">{{ $t('查看来自平台的系统通知、版本公告与重要提醒') }}</p>
        </div>
        <div class="header-actions">
          <el-button @click="loadList" :loading="loading">
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
          <el-button type="primary" :disabled="!hasUnread" :loading="marking" @click="onMarkAllRead">
            <el-icon><Check /></el-icon>
            {{ $t('全部标记已读') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <el-row :gutter="16" class="stats-row">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-value">{{ stats.total || 0 }}</div>
          <div class="stat-label">{{ $t('总消息') }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-unread">
          <div class="stat-value">{{ stats.unread || 0 }}</div>
          <div class="stat-label">{{ $t('未读') }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-info">
          <div class="stat-value">{{ (stats.byType || {}).info || 0 }}</div>
          <div class="stat-label">{{ $t('信息') }}</div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card stat-warning">
          <div class="stat-value">{{ (stats.byType || {}).warning || 0 }}</div>
          <div class="stat-label">{{ $t('警告') }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <el-space>
            <el-select v-model="filter.type" :placeholder="$t('类型')" clearable style="width: 140px" @change="loadList">
              <el-option :label="$t('全部')" :value="''" />
              <el-option :label="$t('信息')" value="info" />
              <el-option :label="$t('警告')" value="warning" />
              <el-option :label="$t('错误')" value="error" />
              <el-option :label="$t('成功')" value="success" />
              <el-option :label="$t('公告')" value="announcement" />
            </el-select>
            <el-select v-model="filter.is_read" :placeholder="$t('状态')" clearable style="width: 120px" @change="loadList">
              <el-option :label="$t('全部')" :value="''" />
              <el-option :label="$t('未读')" :value="0" />
              <el-option :label="$t('已读')" :value="1" />
            </el-select>
            <el-input
              v-model="filter.keyword"
              :placeholder="$t('搜索标题/内容')"
              clearable
              style="width: 220px"
              @keyup.enter="loadList"
              @clear="loadList"
            />
          </el-space>
        </div>
      </template>

      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column label="状态" width="70" align="center">
          <template #default="{ row }">
            <el-badge v-if="!row.is_read" is-dot />
            <el-icon v-else color="#10B981"><Check /></el-icon>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="typeTagType(row.type)" size="small">
              {{ typeLabel(row.type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip />
        <el-table-column prop="content" label="内容" min-width="320" show-overflow-tooltip />
        <el-table-column label="时间" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at || row.createdAt) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="onView(row)">查看</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="total > pageSize"
        :current-page="page"
        :page-size="pageSize"
        :page-sizes="[10, 20, 50]"
        :total="total"
        layout="total, sizes, prev, pager, next, jumper"
        style="margin-top: 16px; text-align: right"
        @size-change="(s) => { pageSize = s; loadList() }"
        @current-change="(p) => { page = p; loadList() }"
      />
    </el-card>

    
    <el-dialog v-model="viewVisible" :title="current?.title || '通知详情'" width="560px">
      <div v-if="current" class="detail-content">
        <el-tag :type="typeTagType(current.type)" size="small">
          {{ typeLabel(current.type) }}
        </el-tag>
        <h3 class="detail-title">{{ current.title }}</h3>
        <p class="detail-time">{{ formatTime(current.created_at || current.createdAt) }}</p>
        <div class="detail-body">{{ current.content }}</div>
        <div v-if="current.link" class="detail-link">
          <el-link type="primary" :href="current.link" target="_blank">
            <el-icon><Link /></el-icon>
            查看详情
          </el-link>
        </div>
      </div>
      <template #footer>
        <el-button @click="viewVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Check, Link } from '@element-plus/icons-vue'
import { http } from '@/utils/request'
import { platformAPI } from '@/api/platform'

const loading = ref(false)
void Refresh; void Check; void Link
const marking = ref(false)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filter = reactive({ type: '', is_read: '', keyword: '' })

const viewVisible = ref(false)
const current = ref(null)

const stats = ref({
  total: 0,
  unread: 0,
  byType: { info: 0, warning: 0, error: 0, success: 0, announcement: 0 }
})

const hasUnread = computed(() => (stats.value.unread || 0) > 0)

const typeLabel = (t) => {
  const map = { info: '信息', warning: '警告', error: '错误', success: '成功', announcement: '公告', notification: '通知' }
  return map[t] || t || '通知'
}

const typeTagType = (t) => {
  const map = { info: 'info', warning: 'warning', error: 'danger', success: 'success', announcement: 'primary' }
  return map[t] || 'info'
}

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const loadList = async () => {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize.value
    };
    if (filter.type) params.type = filter.type
    if (filter.is_read !== '') params.is_read = filter.is_read
    if (filter.keyword) params.keyword = filter.keyword

    let res = null
    try {
      res = await http.get('/api/auth/notifications', { params })
    } catch (e1) {
      try {
        const latest = await platformAPI.getLatestMessage()
        res = latest ? { list: [latest.data || latest], total: 1 } : { list: [], total: 0 }
      } catch (e2) {
        res = { list: [], total: 0 }
      }
    }
    list.value = res?.list || []
    total.value = res?.total || list.value.length
    computeStats();
  } catch (err) {
    ElMessage.error('加载通知失败：' + (err?.message || err))
  } finally {
    loading.value = false
  }
}

const computeStats = () => {
  stats.value.total = list.value.length
  stats.value.unread = list.value.filter(m => !m.is_read).length
  const byType = { info: 0, warning: 0, error: 0, success: 0, announcement: 0 }
  list.value.forEach(m => {
    const t = m.type || 'info'
    byType[t] = (byType[t] || 0) + 1
  })
  stats.value.byType = byType
}

const onView = async (row) => {
  current.value = row
  viewVisible.value = true
  if (!row.is_read) {
    try {
      await platformAPI.markMessageRead(row.id)
      row.is_read = true
      computeStats()
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }
}

const onMarkAllRead = async () => {
  marking.value = true
  try {
    const ids = list.value.filter(m => !m.is_read).map(m => m.id).filter(Boolean)
    for (const id of ids) {
      try {
        await platformAPI.markMessageRead(id)
      } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    }
    list.value.forEach(m => { m.is_read = true })
    computeStats()
    ElMessage.success(i18n.global.t('已全部标记为已读'))
  } catch (err) {
    ElMessage.error('操作失败：' + (err?.message || err))
  } finally {
    marking.value = false
  }
}

onMounted(() => {
  loadList()
})
</script>

<style scoped>
.notifications-page {
  padding: 0;
}
.header-card {
  margin-bottom: 16px;
}
.header-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-content h2 {
  margin: 0 0 4px;
  font-size: 20px;
}
.subtitle {
  color: #909399;
  font-size: 13px;
  margin: 0;
}
.stats-row {
  margin-bottom: 16px;
}
.stat-card {
  text-align: center;
  padding: 8px;
}
.stat-value {
  font-size: 28px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 4px;
}
.stat-label {
  font-size: 13px;
  color: #909399;
}
.stat-unread .stat-value { color: #EF4444; }
.stat-info .stat-value { color: #909399; }
.stat-warning .stat-value { color: #F59E0B; }
.detail-content {
  padding: 8px 0;
}
.detail-title {
  margin: 12px 0 4px;
  font-size: 16px;
  color: #303133;
}
.detail-time {
  color: #909399;
  font-size: 12px;
  margin: 0 0 16px;
}
.detail-body {
  background: #f5f7fa;
  padding: 12px 16px;
  border-radius: 6px;
  font-size: 14px;
  line-height: 1.6;
  color: #303133;
  white-space: pre-wrap;
  word-break: break-word;
}
.detail-link {
  margin-top: 12px;
}
</style>
