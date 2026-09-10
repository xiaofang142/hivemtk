<template>
  <div class="inbox-page">
    <el-card class="header-card" shadow="never">
      <div class="page-header">
        <div class="header-text">
          <h2>{{ $t('统一收件箱') }}</h2>
          <p class="subtitle">{{ $t('跨渠道会话工作台：未读跟踪、坐席分配、对话线程集中处理') }}</p>
        </div>
        <div class="header-actions">
          <el-button @click="loadStats">
            <el-icon><DataAnalysis /></el-icon>
            {{ $t('刷新统计') }}
          </el-button>
          <el-button type="warning" plain @click="handleReconcile('backfill')">
            <el-icon><Refresh /></el-icon>
            {{ $t('会话补建') }}
          </el-button>
          <el-button type="primary" @click="refreshAll">
            <el-icon><RefreshRight /></el-icon>
            {{ $t('刷新') }}
          </el-button>
        </div>
      </div>
      <div class="stats-row" v-if="stats">
        <el-tag type="info" effect="plain">{{ $t('总') }}: {{ stats.total ?? 0 }}</el-tag>
        <el-tag type="danger" effect="plain">{{ $t('未读') }}: {{ stats.unread ?? 0 }}</el-tag>
        <el-tag type="warning" effect="plain">{{ $t('待处理') }}: {{ stats.open ?? 0 }}</el-tag>
        <el-tag type="primary" effect="plain">{{ $t('已分配') }}: {{ stats.assigned ?? 0 }}</el-tag>
        <el-tag type="success" effect="plain">{{ $t('已关闭') }}: {{ stats.closed ?? 0 }}</el-tag>
        <el-tag v-if="(stats.overdue_count || 0) > 0" type="danger" effect="dark">
          {{ $t('超时未响应') }}: {{ stats.overdue_count }}
        </el-tag>
      </div>
    </el-card>

    <div class="workbench">
      <!-- 左：会话列表 -->
      <el-card class="conv-panel" shadow="never">
        <div class="conv-filters">
          <el-select v-model="searchForm.platform" :placeholder="$t('全部平台')" clearable size="default" @change="handleSearch" style="width: 130px">
            <el-option v-for="c in CHANNEL_OPTIONS" :key="c.value" :label="c.label" :value="c.value" />
          </el-select>
          <el-select v-model="searchForm.status" :placeholder="$t('全部状态')" clearable @change="handleSearch" style="width: 120px">
            <el-option :label="$t('未读')" value="unread" />
            <el-option :label="$t('待处理')" value="open" />
            <el-option :label="$t('已分配')" value="assigned" />
            <el-option :label="$t('已关闭')" value="closed" />
          </el-select>
          <el-select v-model="searchForm.pinned" :placeholder="$t('置顶筛选')" clearable @change="handleSearch" style="width: 120px">
            <el-option :label="$t('仅置顶')" value="true" />
            <el-option :label="$t('未置顶')" value="false" />
          </el-select>
          <el-input
            v-model="searchForm.keyword"
            :placeholder="$t('客户名/ID')"
            clearable
            style="flex: 1; min-width: 130px"
            @keyup.enter="handleSearch"
          >
            <template #append>
              <el-button :icon="Search" @click="handleSearch" />
            </template>
          </el-input>
        </div>

        <div v-loading="loading" class="conv-list">
          <div
            v-for="conv in conversations"
            :key="conv.id"
            class="conv-item"
            :class="{ active: currentConv && currentConv.id === conv.id }"
            @click="selectConversation(conv)"
          >
            <div class="conv-item-top">
              <span class="conv-name">
                <el-icon v-if="conv.pinned" class="pin-icon"><Top /></el-icon>
                <el-icon v-if="conv.muted" class="mute-icon"><Mute /></el-icon>
                {{ conv.customer_name || conv.customer_id || '-' }}
              </span>
              <span class="conv-time">{{ formatTime(conv.last_message_at) }}</span>
            </div>
            <div class="conv-item-mid">
              <span class="conv-preview">{{ conv.last_message_preview || $t('暂无消息') }}</span>
              <el-badge v-if="conv.unread_count > 0" :value="conv.unread_count" :max="99" class="conv-badge" />
            </div>
            <div class="conv-item-bottom">
              <el-tag size="small" :type="getChannelTagType(conv.platform) || 'info'">{{ getChannelLabel(conv.platform) || conv.platform }}</el-tag>
              <el-tag size="small" :type="statusTagType(conv.status)">{{ statusLabel(conv.status) }}</el-tag>
              <el-tag v-if="conv.assigned_to" size="small" type="primary" effect="plain">
                {{ $t('坐席') }}: {{ conv.assigned_to }}
              </el-tag>
              <el-tag v-for="t in (conv.tags || []).slice(0, 3)" :key="t" size="small" type="warning" effect="plain" closable @close.stop="handleRemoveTag(conv, t)">
                {{ t }}
              </el-tag>
              <el-icon
                v-if="conv.starred"
                class="star-icon"
                @click.stop="handleToggleStar(conv)"
              ><StarFilled /></el-icon>
            </div>
          </div>
          <el-empty v-if="!loading && conversations.length === 0" :description="$t('暂无会话')" />
        </div>

        <div class="conv-pagination">
          <el-pagination
            layout="prev, pager, next"
            small
            background
            :total="pagination.total"
            :page-size="pagination.pageSize"
            :current-page="pagination.page"
            @current-change="handlePageChange"
          />
        </div>
      </el-card>

      <!-- 右：对话线程 -->
      <el-card class="thread-panel" shadow="never">
        <template #header>
          <div class="thread-header" v-if="currentConv">
            <div class="thread-title">
              <span class="thread-name">{{ currentConv.customer_name || currentConv.customer_id || '-' }}</span>
              <el-tag size="small" :type="getChannelTagType(currentConv.platform) || 'info'">{{ getChannelLabel(currentConv.platform) || currentConv.platform }}</el-tag>
              <el-tag size="small" :type="statusTagType(currentConv.status)">{{ statusLabel(currentConv.status) }}</el-tag>
            </div>
            <div class="thread-actions">
              <el-tooltip :content="currentConv.pinned ? $t('取消置顶') : $t('置顶')">
                <el-button size="small" :type="currentConv.pinned ? 'primary' : 'default'" :icon="Top" circle @click="handleTogglePin(currentConv)" />
              </el-tooltip>
              <el-tooltip :content="currentConv.starred ? $t('取消星标') : $t('星标')">
                <el-button size="small" :type="currentConv.starred ? 'warning' : 'default'" :icon="currentConv.starred ? StarFilled : Star" circle @click="handleToggleStar(currentConv)" />
              </el-tooltip>
              <el-tooltip :content="currentConv.muted ? $t('取消静音') : $t('静音')">
                <el-button size="small" :type="currentConv.muted ? 'info' : 'default'" :icon="Mute" circle @click="handleToggleMute(currentConv)" />
              </el-tooltip>
              <el-button size="small" @click="tagDialogVisible = true">{{ $t('标签') }}</el-button>
              <el-dropdown trigger="click" @command="handleAssignCommand">
                <el-button size="small" type="primary">
                  {{ $t('分配') }}<el-icon class="el-icon--right"><ArrowDown /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="assign">{{ $t('分配给坐席…') }}</el-dropdown-item>
                    <el-dropdown-item command="auto-least">{{ $t('自动分配（最少负载）') }}</el-dropdown-item>
                    <el-dropdown-item command="auto-rr">{{ $t('自动分配（轮询）') }}</el-dropdown-item>
                    <el-dropdown-item divided command="release">{{ $t('释放') }}</el-dropdown-item>
                    <el-dropdown-item command="close">{{ $t('关闭会话') }}</el-dropdown-item>
                    <el-dropdown-item command="reopen">{{ $t('重开会话') }}</el-dropdown-item>
                    <el-dropdown-item divided command="history">{{ $t('分配历史') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
          <div v-else class="thread-header-empty">{{ $t('选择左侧会话查看对话') }}</div>
        </template>

        <div v-if="currentConv" v-loading="threadLoading" class="thread-body" ref="threadBodyRef">
          <div
            v-for="msg in threadMessages"
            :key="`${msg.source}-${msg.id}`"
            class="msg-row"
            :class="{ mine: isOutbound(msg) }"
          >
            <div class="msg-bubble">
              <div class="msg-meta">
                <span class="msg-sender">{{ msg.sender_name || msg.sender_id || '-' }}</span>
                <el-tag v-if="msg.is_ai_reply" size="small" type="success" effect="plain">AI</el-tag>
                <el-tag size="small" :type="msg.source === 'hub' ? 'info' : 'warning'" effect="plain">
                  {{ msg.source === 'hub' ? $t('渠道') : $t('客服') }}
                </el-tag>
                <span class="msg-time">{{ formatTime(msg.sent_at || msg.created_at) }}</span>
                <el-popconfirm :title="$t('确认删除这条消息？')" @confirm="handleDeleteMessage(msg)">
                  <template #reference>
                    <el-button size="small" type="danger" link :icon="Delete" class="msg-del" />
                  </template>
                </el-popconfirm>
              </div>
              <div class="msg-content">
                <el-image
                  v-if="isImage(msg)"
                  :src="msg.media_url"
                  :preview-src-list="[msg.media_url]"
                  fit="cover"
                  class="msg-image"
                  lazy
                />
                <template v-else>{{ msg.content || '-' }}</template>
              </div>
            </div>
          </div>
          <el-empty v-if="!threadLoading && threadMessages.length === 0" :description="$t('该会话暂无消息')" />
        </div>
        <el-empty v-else :description="$t('选择左侧会话查看对话')" />

        <div v-if="currentConv" class="thread-pagination">
          <el-pagination
            layout="prev, pager, next, total"
            small
            background
            :total="threadPagination.total"
            :page-size="threadPagination.pageSize"
            :current-page="threadPagination.page"
            @current-change="handleThreadPageChange"
          />
        </div>
      </el-card>
    </div>

    <!-- 分配给坐席对话框 -->
    <el-dialog v-model="assignDialogVisible" :title="$t('分配会话给坐席')" width="460px">
      <el-form label-width="90px">
        <el-form-item :label="$t('坐席')">
          <el-select v-model="assignForm.to_user_id" filterable :placeholder="$t('选择坐席')" style="width: 100%">
            <el-option v-for="s in staffOptions" :key="s.username" :label="`${s.nickname || s.username}`" :value="s.username" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('备注')">
          <el-input v-model="assignForm.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="assignDialogVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="assignSubmitting" @click="submitAssign">{{ $t('确定') }}</el-button>
      </template>
    </el-dialog>

    <!-- 标签管理对话框 -->
    <el-dialog v-model="tagDialogVisible" :title="$t('会话标签')" width="420px">
      <div class="tag-list" v-if="currentConv">
        <el-tag
          v-for="t in (currentConv.tags || [])"
          :key="t"
          closable
          type="warning"
          effect="plain"
          style="margin: 0 8px 8px 0"
          @close="handleRemoveTag(currentConv, t)"
        >{{ t }}</el-tag>
        <span v-if="!(currentConv.tags || []).length" class="tag-empty">{{ $t('暂无标签') }}</span>
      </div>
      <el-input v-model="newTag" :placeholder="$t('输入标签后回车添加')" @keyup.enter="handleAddTag" clearable>
        <template #append>
          <el-button @click="handleAddTag">{{ $t('添加') }}</el-button>
        </template>
      </el-input>
      <template #footer>
        <el-button @click="tagDialogVisible = false">{{ $t('关闭') }}</el-button>
      </template>
    </el-dialog>

    <!-- 分配历史对话框 -->
    <el-dialog v-model="historyDialogVisible" :title="$t('分配历史')" width="640px">
      <el-table :data="assignmentHistory" border size="small" v-loading="historyLoading">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="action" :label="$t('动作')" width="90">
          <template #default="{ row }">{{ actionLabel(row.action) }}</template>
        </el-table-column>
        <el-table-column prop="to_type" :label="$t('对象类型')" width="90">
          <template #default="{ row }">{{ toTypeLabel(row.to_type) }}</template>
        </el-table-column>
        <el-table-column prop="to_user_id" :label="$t('坐席/SOP')" min-width="110">
          <template #default="{ row }">{{ row.to_user_id || (row.to_sop_id ? `SOP#${row.to_sop_id}` : '-') }}</template>
        </el-table-column>
        <el-table-column prop="operator_id" :label="$t('操作人')" min-width="100">
          <template #default="{ row }">{{ row.operator_id || '-' }}</template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('时间')" min-width="150">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Search, RefreshRight, Refresh, DataAnalysis, Top, Star, StarFilled, Mute,
  Delete, ArrowDown
} from '@element-plus/icons-vue'
import { inboxApi } from '@/api/inbox'
import { listSystemUsers } from '@/api/systemUser'
import { useUserStore } from '@/stores/user'
import { CHANNEL_OPTIONS, getChannelLabel, getChannelTagType } from '@/constants/channel'
import i18n from '@/i18n'

const t = (k) => i18n.global.t(k)

const loading = ref(false)
const threadLoading = ref(false)
const conversations = ref([])
const currentConv = ref(null)
const threadMessages = ref([])
const threadBodyRef = ref(null)
const stats = ref(null)

const searchForm = reactive({
  platform: '',
  status: '',
  pinned: '',
  keyword: ''
})

const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const threadPagination = reactive({ page: 1, pageSize: 50, total: 0 })

const assignDialogVisible = ref(false)
const assignSubmitting = ref(false)
const assignForm = reactive({ to_user_id: '', remark: '' })
const staffOptions = ref([])

const tagDialogVisible = ref(false)
const newTag = ref('')

const historyDialogVisible = ref(false)
const historyLoading = ref(false)
const assignmentHistory = ref([])

const userStore = useUserStore()
let refreshTimer = null

// ---- 字典 ----
const STATUS_MAP = {
  unread: { label: '未读', type: 'danger' },
  open: { label: '待处理', type: 'warning' },
  assigned: { label: '已分配', type: 'primary' },
  closed: { label: '已关闭', type: 'success' }
}
// 非字典值（历史脏数据）归一到未读/待处理显示，避免出现原始英文状态
const statusLabel = (v) => {
  if (STATUS_MAP[v]) return t(STATUS_MAP[v].label)
  if (!v) return '-'
  return t((v.unread_count ?? 0) > 0 ? '未读' : '待处理')
}
const statusTagType = (v) => (STATUS_MAP[v] ? STATUS_MAP[v].type : 'danger')

const ACTION_MAP = {
  assign: '分配', reassign: '改派', release: '释放', close: '关闭', reopen: '重开'
}
const actionLabel = (v) => (ACTION_MAP[v] ? t(ACTION_MAP[v]) : (v || '-'))

const TOTYPE_MAP = { human: '人工', sop: 'SOP', ai: 'AI' }
const toTypeLabel = (v) => (TOTYPE_MAP[v] ? t(TOTYPE_MAP[v]) : (v || '-'))

const isImage = (msg) =>
  (msg.content_type === 'image' || /\.(png|jpe?g|gif|webp)(\?|$)/i.test(msg.media_url || '')) && !!msg.media_url

// outbound: hub 表 direction 字段在 GetMessages 输出中没有直接暴露，
// 通过 is_ai_reply 或 sender 与账号无关的客服侧推断；这里以 sender_id 等于会话账号 id 视为坐席侧
const isOutbound = (msg) => {
  if (msg.is_ai_reply) return true
  if (msg.sender_type === 'staff' || msg.sender_type === 'ai' || msg.sender_type === 'system') return true
  if (msg.source === 'hub' && currentConv.value) {
    return msg.sender_id && msg.sender_id !== currentConv.value.customer_id &&
      msg.sender_id === currentConv.value.account_id
  }
  return false
}

const formatTime = (v) => {
  if (!v) return '-'
  const d = new Date(v)
  if (isNaN(d.getTime())) return String(v)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return sameDay ? hm : `${d.getMonth() + 1}/${d.getDate()} ${hm}`
}

// ---- 数据加载 ----
const fetchConversations = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize
    }
    if (searchForm.platform) params.platform = searchForm.platform
    if (searchForm.status) params.status = searchForm.status
    if (searchForm.keyword) params.keyword = searchForm.keyword
    if (searchForm.pinned !== '' && searchForm.pinned !== null && searchForm.pinned !== undefined) {
      params.pinned = searchForm.pinned
    }
    const res = await inboxApi.listConversations(params)
    const data = res?.data || res || {}
    conversations.value = data.list || []
    pagination.total = data.total || 0
  } catch (e) {
    console.error('[inbox] fetchConversations', e)
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res = await inboxApi.getStats()
    stats.value = res?.data || res || null
  } catch (e) {
    console.error('[inbox] loadStats', e)
  }
}

const fetchThread = async () => {
  if (!currentConv.value) return
  threadLoading.value = true
  try {
    const res = await inboxApi.getMessages(currentConv.value.id, {
      page: threadPagination.page,
      page_size: threadPagination.pageSize
    })
    const data = res?.data || res || {}
    // 后端按时间倒序返回，前端展示为正序对话
    threadMessages.value = (data.list || []).slice().reverse()
    threadPagination.total = data.total || 0
    await nextTick()
    scrollThreadToBottom()
  } catch (e) {
    console.error('[inbox] fetchThread', e)
  } finally {
    threadLoading.value = false
  }
}

const scrollThreadToBottom = () => {
  const el = threadBodyRef.value
  if (el?.$el) el.$el.scrollTop = el.$el.scrollHeight
  else if (el) el.scrollTop = el.scrollHeight
}

const selectConversation = async (conv) => {
  currentConv.value = conv
  threadPagination.page = 1
  threadPagination.total = 0
  threadMessages.value = []
  fetchThread()
  // 有未读则顺带标记已读（历史脏状态如 'active' 一并按有未读处理）
  const isUnreadLike = conv.status === 'unread' || !['open', 'assigned', 'closed'].includes(conv.status)
  if (conv.unread_count > 0 || isUnreadLike) {
    try {
      await inboxApi.markRead(conv.id)
      conv.unread_count = 0
      if (isUnreadLike) conv.status = 'open'
    } catch (e) { console.warn('[inbox] markRead', e) }
  }
}

const refreshAll = () => {
  fetchConversations()
  loadStats()
  if (currentConv.value) fetchThread()
}

const handleSearch = () => {
  pagination.page = 1
  fetchConversations()
}

const handlePageChange = (p) => {
  pagination.page = p
  fetchConversations()
}

const handleThreadPageChange = (p) => {
  threadPagination.page = p
  fetchThread()
}

// ---- 会话操作 ----
const refreshCurrentInPlace = async () => {
  if (!currentConv.value) return
  try {
    const res = await inboxApi.getConversation(currentConv.value.id)
    const fresh = res?.data || res
    if (fresh && fresh.id) {
      const idx = conversations.value.findIndex((c) => c.id === fresh.id)
      if (idx >= 0) conversations.value.splice(idx, 1, fresh)
      currentConv.value = fresh
    }
  } catch (e) { console.warn('[inbox] refreshCurrent', e) }
}

const handleTogglePin = async (conv) => {
  try {
    await inboxApi.setPin(conv.id, !conv.pinned)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] pin', e) }
}

const handleToggleStar = async (conv) => {
  try {
    await inboxApi.setStar(conv.id, !conv.starred)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] star', e) }
}

const handleToggleMute = async (conv) => {
  try {
    await inboxApi.setMute(conv.id, !conv.muted)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] mute', e) }
}

const handleAddTag = async () => {
  const tag = (newTag.value || '').trim()
  if (!tag || !currentConv.value) return
  try {
    await inboxApi.addTag(currentConv.value.id, tag)
    newTag.value = ''
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] addTag', e) }
}

const handleRemoveTag = async (conv, tag) => {
  try {
    await inboxApi.removeTag(conv.id, tag)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] removeTag', e) }
}

const handleDeleteMessage = async (msg) => {
  if (!currentConv.value) return
  try {
    await inboxApi.deleteMessage(currentConv.value.id, msg.id, msg.source || 'hub')
    ElMessage.success(t('删除成功'))
    fetchThread()
  } catch (e) { console.error('[inbox] deleteMessage', e) }
}

// ---- 分配 ----
const loadStaffOptions = async () => {
  if (staffOptions.value.length) return
  try {
    const res = await listSystemUsers({ page: 1, page_size: 100 })
    const data = res?.data || res || {}
    staffOptions.value = data.list || data || []
  } catch (e) {
    console.warn('[inbox] loadStaff', e)
    staffOptions.value = []
  }
}

const handleAssignCommand = async (cmd) => {
  if (!currentConv.value) return
  const conv = currentConv.value
  if (cmd === 'assign') {
    assignForm.to_user_id = ''
    assignForm.remark = ''
    await loadStaffOptions()
    assignDialogVisible.value = true
    return
  }
  if (cmd === 'auto-least' || cmd === 'auto-rr') {
    await loadStaffOptions()
    const candidates = staffOptions.value.map((s) => s.username).filter(Boolean)
    if (!candidates.length) {
      ElMessage.warning(t('暂无可分配坐席'))
      return
    }
    try {
      await inboxApi.autoAssign({
        conversation_id: conv.id,
        candidates,
        mode: cmd === 'auto-rr' ? 'round_robin' : 'least_busy'
      })
      ElMessage.success(t('分配成功'))
      refreshAll()
    } catch (e) { console.error('[inbox] autoAssign', e) }
    return
  }
  if (cmd === 'history') {
    historyDialogVisible.value = true
    historyLoading.value = true
    try {
      const res = await inboxApi.listAssignments({ conversation_id: conv.id, page: 1, page_size: 50 })
      const data = res?.data || res || {}
      assignmentHistory.value = data.list || []
    } catch (e) {
      console.error('[inbox] assignments', e)
      assignmentHistory.value = []
    } finally {
      historyLoading.value = false
    }
    return
  }
  // release / close / reopen
  try {
    await inboxApi.assign({
      conversation_id: conv.id,
      action: cmd
    })
    ElMessage.success(t('操作成功'))
    refreshAll()
  } catch (e) { console.error('[inbox] assign', cmd, e) }
}

const submitAssign = async () => {
  if (!assignForm.to_user_id) {
    ElMessage.warning(t('请选择坐席'))
    return
  }
  assignSubmitting.value = true
  try {
    await inboxApi.assign({
      conversation_id: currentConv.value.id,
      action: currentConv.value.assigned_to ? 'reassign' : 'assign',
      to_type: 'human',
      to_user_id: assignForm.to_user_id,
      remark: assignForm.remark
    })
    ElMessage.success(t('分配成功'))
    assignDialogVisible.value = false
    refreshAll()
  } catch (e) {
    console.error('[inbox] submitAssign', e)
  } finally {
    assignSubmitting.value = false
  }
}

// ---- 对账 ----
const handleReconcile = async (mode) => {
  try {
    const res = await inboxApi.reconcile(mode)
    const data = res?.data || res || {}
    ElMessage.success(data.message || t('对账完成'))
    refreshAll()
  } catch (e) { console.error('[inbox] reconcile', e) }
}

// ---- 轮询刷新（30s，保持未读/新会话感知） ----
const startPolling = () => {
  stopPolling()
  refreshTimer = setInterval(() => {
    fetchConversations()
    loadStats()
  }, 30000)
}
const stopPolling = () => {
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
}

onMounted(() => {
  refreshAll()
  startPolling()
})

onUnmounted(stopPolling)
</script>

<style scoped>
.inbox-page {
  padding: 0;
}
.header-card {
  margin-bottom: 12px;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}
.page-header h2 {
  margin: 0 0 4px;
}
.subtitle {
  margin: 0;
  color: #909399;
  font-size: 13px;
}
.stats-row {
  margin-top: 10px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.workbench {
  display: flex;
  gap: 12px;
  align-items: stretch;
}
.conv-panel {
  width: 420px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
}
.thread-panel {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.conv-filters {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.conv-list {
  min-height: 300px;
  max-height: calc(100vh - 340px);
  overflow-y: auto;
}
.conv-item {
  padding: 10px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  cursor: pointer;
  border-radius: 6px;
  transition: background-color 0.15s;
}
.conv-item:hover {
  background-color: var(--el-fill-color-light);
}
.conv-item.active {
  background-color: var(--el-color-primary-light-9);
}
.conv-item-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.conv-name {
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pin-icon { color: var(--el-color-primary); }
.mute-icon { color: var(--el-color-info); }
.conv-time {
  color: #909399;
  font-size: 12px;
  flex-shrink: 0;
}
.conv-item-mid {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 4px;
}
.conv-preview {
  color: #606266;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.conv-badge { flex-shrink: 0; margin-left: 8px; }
.conv-item-bottom {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-top: 6px;
  flex-wrap: wrap;
}
.star-icon { color: var(--el-color-warning); }
.conv-pagination {
  padding-top: 10px;
  display: flex;
  justify-content: center;
}
.thread-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.thread-header-empty {
  color: #909399;
  text-align: center;
  width: 100%;
}
.thread-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.thread-name {
  font-weight: 600;
  font-size: 15px;
}
.thread-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}
.thread-body {
  min-height: 300px;
  max-height: calc(100vh - 380px);
  overflow-y: auto;
  padding: 8px 4px;
}
.msg-row {
  display: flex;
  margin-bottom: 12px;
}
.msg-row.mine {
  justify-content: flex-end;
}
.msg-bubble {
  max-width: 72%;
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 8px 12px;
}
.msg-row.mine .msg-bubble {
  background: var(--el-color-primary-light-9);
}
.msg-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #909399;
  margin-bottom: 4px;
  flex-wrap: wrap;
}
.msg-sender {
  font-weight: 600;
  color: #606266;
}
.msg-time { flex-shrink: 0; }
.msg-del { margin-left: auto; }
.msg-content {
  font-size: 14px;
  line-height: 1.5;
  word-break: break-word;
  white-space: pre-wrap;
}
.msg-image {
  max-width: 220px;
  max-height: 180px;
  border-radius: 4px;
}
.thread-pagination {
  padding-top: 10px;
  display: flex;
  justify-content: center;
}
.tag-empty { color: #909399; font-size: 13px; }
.tag-list { margin-bottom: 12px; }
</style>
