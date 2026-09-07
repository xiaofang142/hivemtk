<template>
  <div class="chat-channel-list-page">
    
    <el-card class="header-card" shadow="never">
      <div class="header-content">
        <div>
          <h2>客服 Web Widget 渠道</h2>
          <p class="subtitle">管理嵌入到企业网站的客服浮标入口，每个渠道对应一个 AppKey + 一组白名单 origin</p>
        </div>
        <div class="header-actions">
          <el-button @click="loadList" :loading="loading">
            <el-icon><Refresh /></el-icon>
            {{ $t('刷新') }}
          </el-button>
          <el-button type="primary" @click="goCreate">
            <el-icon><Plus /></el-icon>
            {{ $t('新建渠道') }}
          </el-button>
        </div>
      </div>
    </el-card>

    
    <el-card shadow="never" class="filter-card">
      <el-form :inline="true" :model="filter" @submit.prevent>
        <el-form-item :label="$t('关键词')">
          <el-input
            v-model="filter.keyword"
            :placeholder="$t('搜索渠道名 / AppKey')"
            clearable
            style="width: 240px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
        </el-form-item>
        <el-form-item :label="$t('状态')">
          <el-select v-model="filter.status" :placeholder="$t('全部')" clearable style="width: 140px" @change="onSearch">
            <el-option :label="$t('启用')" value="active" />
            <el-option :label="$t('禁用')" value="disabled" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">
            <el-icon><Search /></el-icon>
            {{ $t('搜索') }}
          </el-button>
          <el-button @click="resetFilter">{{ $t('重置') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    
    <el-card shadow="never">
      <el-table :data="list" v-loading="loading" stripe border>
        <el-table-column prop="id" label="ID" width="70" align="center" />
        <el-table-column prop="channel_name" :label="$t('渠道名称')" min-width="160" show-overflow-tooltip />
        <el-table-column label="AppKey" min-width="240">
          <template #default="{ row }">
            <code class="appkey">{{ row.app_key }}</code>
            <el-button link type="primary" size="small" @click="copy(row.app_key)">
              <el-icon><CopyDocument /></el-icon>
            </el-button>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="getStatusTagType(row.status)" size="small">
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="浮标" width="160">
          <template #default="{ row }">
            <div class="widget-preview" :style="{ backgroundColor: row.widget_color }">
              <el-icon><ChatLineRound /></el-icon>
            </div>
            <span class="widget-pos">{{ row.widget_position }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="visitor_count" label="访客" width="80" align="center" />
        <el-table-column prop="session_count" label="会话" width="80" align="center" />
        <el-table-column label="目标语言" width="120" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.target_language" size="small">{{ getLanguageLabel(row.target_language) }}</el-tag>
            <span v-else class="lang-follow">跟随智能体</span>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="320" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="goEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="router.push({ name: 'ChatChannelInstallGuide', params: { id: row.channel_id } })">安装引导</el-button>
            <el-button link type="primary" size="small" @click="onRotateKey(row)">轮换 Key</el-button>
            <el-button link type="warning" size="small" @click="onResetSecret(row)">重置 Secret</el-button>
            <el-button v-if="row.status === 'disabled'" link type="success" size="small" @click="onEnable(row)">启用</el-button>
            <el-button v-else link type="danger" size="small" @click="onDisable(row)">禁用</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        class="pagination"
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadList"
        @current-change="loadList"
      />
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Search, CopyDocument, ChatLineRound } from '@element-plus/icons-vue'
import { listChannels, updateChannel, rotateAppKey, resetAppSecret } from '@/api/chatChannel'
import DOMPurify from 'dompurify'
import { getEnabledLabel, getEnabledTagType } from '@/constants/enabled';
import { getLanguageLabel } from '@/constants/languages'

const getStatusLabel = (s) => getEnabledLabel(s)
const getStatusTagType = (s) => getEnabledTagType(s)

const router = useRouter()
const loading = ref(false)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filter = ref({ keyword: '', status: '' })

const loadList = async () => {
  loading.value = true
  try {
    const res = await listChannels({
      page: page.value,
      page_size: pageSize.value,
      keyword: filter.value.keyword,
      status: filter.value.status
    })
    list.value = res?.list || []
    total.value = res?.total || 0
  } catch (err) {
    ElMessage.error('加载失败：' + err.message)
  } finally {
    loading.value = false
  }
}

const onSearch = () => {
  page.value = 1
  loadList()
}

const resetFilter = () => {
  filter.value = { keyword: '', status: '' }
  page.value = 1
  loadList()
}

const goCreate = () => router.push({ name: 'ChatChannelCreate' })
const goEdit = (row) => router.push({ name: 'ChatChannelEdit', params: { id: row.channel_id } })

const onDisable = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认禁用渠道「${row.channel_name}」？禁用后该 AppKey 将无法创建新会话（可随时重新启用）。`,
      '禁用确认',
      { type: 'warning' }
    )
    await updateChannel(row.channel_id, { status: 'disabled' })
    ElMessage.success(i18n.global.t('已禁用'))
    loadList()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('禁用失败：' + (err?.message || err))
    }
  }
}

const onEnable = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认启用渠道「${row.channel_name}」？启用后该 AppKey 可正常创建新会话。`,
      '启用确认',
      { type: 'warning' }
    )
    await updateChannel(row.channel_id, { status: 'active' })
    ElMessage.success(i18n.global.t('已启用'))
    loadList()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('启用失败：' + (err?.message || err))
    }
  }
}

const onRotateKey = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认轮换「${row.channel_name}」的 AppKey？轮换后旧 AppKey 立即失效。`,
      '轮换 AppKey',
      { type: 'warning' }
    )
    const res = await rotateAppKey(row.channel_id)
    ElMessageBox.alert(
      DOMPurify.sanitize(`<p>新 AppKey：</p><pre style="background:#f5f5f5;padding:8px;user-select:all">${res.app_key}</pre><p style="color:#F59E0B">请立即更新嵌入代码中的 AppKey</p>`),
      '轮换成功',
      { dangerouslyUseHTMLString: true }
    )
    loadList()
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('轮换失败：' + (err?.message || err))
    }
  }
}

const onResetSecret = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确认重置「${row.channel_name}」的 AppSecret？重置后旧 Secret 立即失效。`,
      '重置 AppSecret',
      { type: 'warning' }
    )
    const res = await resetAppSecret(row.channel_id)
    ElMessageBox.alert(
      DOMPurify.sanitize(`<p>新 AppSecret（仅显示一次）：</p><pre style="background:#f5f5f5;padding:8px;user-select:all">${res.app_secret}</pre><p style="color:#F59E0B">请妥善保存！关闭后无法再次查看</p>`),
      '重置成功',
      { dangerouslyUseHTMLString: true }
    )
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error('重置失败：' + (err?.message || err))
    }
  }
}

const copy = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(i18n.global.t('已复制到剪贴板'))
  } catch {
    ElMessage.error(i18n.global.t('复制失败'))
  }
}

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

onMounted(loadList)
</script>

<style scoped>
.chat-channel-list-page {
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
.filter-card {
  margin-bottom: 16px;
}
.appkey {
  font-family: 'SF Mono', Monaco, Consolas, monospace;
  font-size: 12px;
  background: #f5f7fa;
  padding: 2px 6px;
  border-radius: 3px;
  user-select: all;
}
.widget-preview {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  color: #fff;
  margin-right: 8px;
  vertical-align: middle;
}
.widget-pos {
  font-size: 12px;
  color: #909399;
}
.lang-follow {
  font-size: 12px;
  color: #909399;
}
.pagination {
  margin-top: 16px;
  text-align: right;
}
</style>
