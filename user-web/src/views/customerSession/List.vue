<template>
  <div class="customer-session-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('客户会话管理') }}</h2>
        <p class="subtitle">统一管理多渠道客户对话 · 集成坐席状态 / 快捷回复 / 标签 / AI 建议 / 三栏看板</p>
      </div>
      <div class="header-actions">

        <el-select
          v-model="myStatus"
          size="default"
          style="width: 130px"
          @change="handleStatusChange"
        >
          <el-option value="online">
            <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#10B981;margin-right:8px;vertical-align:middle;"></span>{{ $t('在线') }}
          </el-option>
          <el-option value="busy">
            <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#F59E0B;margin-right:8px;vertical-align:middle;"></span>{{ $t('忙碌') }}
          </el-option>
          <el-option value="offline">
            <span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#94A3B8;margin-right:8px;vertical-align:middle;"></span>{{ $t('离线') }}
          </el-option>
        </el-select>

        <el-button @click="openBlacklistDialog">
          <el-icon><Warning /></el-icon>
          黑名单管理
        </el-button>
        <el-button type="primary" @click="showCreateSession">
          <el-icon><Plus /></el-icon>
          {{ $t('新建会话') }}
        </el-button>
      </div>
    </el-card>

    <div class="main-content">

      <div class="session-list">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('会话列表') }}</span>
              <el-select v-model="filterStatus" size="small" style="width: 120px">
                <el-option :label="$t('全部')" value="" />
                <el-option :label="$t('进行中')" value="__active__" />
                <el-option :label="$t('待处理')" value="pending" />
                <el-option :label="$t('AI处理')" value="ai_handling" />
                <el-option :label="$t('人工')" value="human_handling" />
                <el-option :label="$t('等待')" value="waiting" />
                <el-option :label="$t('已解决')" value="resolved" />
                <el-option :label="$t('已关闭')" value="closed" />
              </el-select>
            </div>
          </template>
          <div
            v-for="session in filteredSessions"
            :key="session.id"
            class="session-item"
            :class="{ active: currentSession?.id === session.id, blacklisted: blacklistedSessionIds.includes(session.id) }"
            @click="selectSession(session)"
          >
            <el-avatar :size="40">{{ session.customerName?.charAt(0) }}</el-avatar>
            <div class="session-info">
              <div class="session-top">
                <span class="name">{{ session.customerName }}</span>
                <span class="time">{{ formatTime(session.lastTime) }}</span>
              </div>
              <div class="preview">{{ session.lastMessage }}</div>
              <div class="session-meta">

                <el-tag
                  :type="session.handlerType === 'human' ? 'success' : 'info'"
                  size="small"
                  effect="plain"
                >
                  <el-icon style="vertical-align: middle">
                    <component :is="session.handlerType === 'human' ? User : MagicStick" />
                  </el-icon>
                  {{ session.handlerType === 'human' ? '人工' : 'AI' }}
                </el-tag>
                <el-tag size="small" effect="plain">{{ getChannelLabel(session.channel) }}</el-tag>

                <el-tag
                  v-if="blacklistedSessionIds.includes(session.id)"
                  size="small"
                  type="danger"
                  effect="dark"
                >已拉黑</el-tag>
              </div>
            </div>
            <el-badge v-if="session.unread" :value="session.unread" />
          </div>
          <el-empty v-if="filteredSessions.length === 0" description="暂无会话" :image-size="80" />
        </el-card>
      </div>


      <div class="chat-area">
        <el-card v-if="currentSession">

          <template #header>
            <div class="chat-header">
              <div class="customer-info">
                <h3>{{ currentSession.customerName }}</h3>
                <span class="channel">{{ getChannelLabel(currentSession.channel) }}</span>

                <el-tag
                  :type="currentHandler === 'human' ? 'success' : 'info'"
                  size="small"
                  effect="dark"
                  class="handler-tag"
                >
                  <span class="status-dot" :class="currentHandler" />
                  <el-icon style="vertical-align: middle">
                    <component :is="currentHandler === 'human' ? User : MagicStick" />
                  </el-icon>
                  {{ currentHandler === 'human' ? '人工接管' : 'AI 托管' }}
                </el-tag>

                <div class="session-tags">
                  <el-tag
                    v-for="tag in sessionTags"
                    :key="tag.id"
                    :color="tag.color || '#4F46E5'"
                    effect="dark"
                    size="small"
                    closable
                    @close="removeSessionTag(tag)"
                    style="color: #fff; margin-right: 6px"
                  >
                    {{ tag.name }}
                  </el-tag>
                  <el-select
                    v-model="tagToAdd"
                    placeholder="+ 添加标签"
                    size="small"
                    style="width: 110px"
                    clearable
                    filterable
                    @change="addSessionTag"
                  >
                    <el-option
                      v-for="t in allTags"
                      :key="t.id"
                      :label="t.name"
                      :value="t.id"
                    />
                  </el-select>
                </div>
              </div>
              <div class="header-actions">

                <el-button
                  v-if="currentHandler === 'ai'"
                  type="success"
                  size="small"
                  :icon="User"
                  :loading="handlerSwitching"
                  @click="handleTakeover"
                >
                  接管会话
                </el-button>
                <el-button
                  v-else
                  type="warning"
                  size="small"
                  :icon="MagicStick"
                  :loading="handlerSwitching"
                  @click="handleRelease"
                >
                  释放回 AI
                </el-button>
                <el-button size="small" @click="closeSession">结束会话</el-button>
              </div>
            </div>
          </template>


          <div v-if="aiSuggestions.length > 0" class="ai-suggestion-bar">
            <div class="ai-bar-title">
              <el-icon style="color: #4F46E5"><MagicStick /></el-icon>
              <span>AI 建议 ({{ aiSuggestions.length }})</span>
              <el-button link size="small" @click="loadAiSuggestions" style="margin-left: auto">
                刷新
              </el-button>
            </div>
            <div class="ai-suggestions">
              <div
                v-for="s in aiSuggestions.slice(0, 3)"
                :key="s.id"
                class="ai-suggestion-item"
                @click="useAiSuggestion(s)"
              >
                <div class="ai-text">{{ s.suggestion }}</div>
                <div class="ai-meta">
                  <el-tag size="small" :type="s.is_used ? 'success' : 'info'">
                    {{ s.is_used ? '已采纳' : '置信度 ' + Math.round((s.confidence || 0) * 100) + '%' }}
                  </el-tag>
                  <el-button
                    v-if="!s.is_used"
                    link
                    type="primary"
                    size="small"
                    @click.stop="useAiSuggestion(s)"
                  >采纳并发送</el-button>
                </div>
              </div>
            </div>
          </div>

          <div class="messages" ref="messagesRef">
            <div
              v-for="msg in messages"
              :key="msg.id"
              class="message"
              :class="{ mine: msg.direction === 'out' }"
            >
              <el-avatar :size="36" class="avatar">
                <el-icon v-if="msg.senderType === 'ai'"><MagicStick /></el-icon>
                <span v-else>{{ msg.from?.charAt(0) }}</span>
              </el-avatar>
              <div class="bubble">
                <div class="sender-tag" v-if="msg.senderType === 'ai'">AI 助手</div>
                <div class="sender-tag agent" v-else-if="msg.senderType === 'agent'">{{ msg.from }}</div>
                <div class="content">{{ msg.content }}</div>
                <div class="time">{{ msg.createdAt }}</div>
              </div>
            </div>
          </div>


          <div class="input-area">
            <div v-if="quickReplies.length > 0" class="quick-replies-bar">
              <el-dropdown
                trigger="click"
                @command="insertQuickReply"
              >
                <el-button size="small" plain>
                  <el-icon><ChatLineSquare /></el-icon>
                  快捷回复 ({{ quickReplies.length }})
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item
                      v-for="r in quickReplies"
                      :key="r.id"
                      :command="r"
                    >
                      <div class="quick-reply-item">
                        <div class="qr-title">
                          <el-tag size="small">{{ r.category || '通用' }}</el-tag>
                          <span>{{ r.title }}</span>
                        </div>
                        <div class="qr-content">{{ r.content }}</div>
                      </div>
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
              <el-input
                v-model="quickReplySearch"
                size="small"
                placeholder="搜索关键词触发快捷回复..."
                style="flex: 1; margin-left: 10px"
                clearable
                @input="onQuickReplySearch"
              />
            </div>


            <el-alert
              v-if="currentHandler === 'ai'"
              type="info"
              :closable="false"
              show-icon
              style="margin-bottom: 10px"
            >
              <template #title>
                AI 正在处理该会话。如需人工回复，请先点击右上角 <b>「接管会话」</b> 按钮。
              </template>
            </el-alert>

            <el-input
              v-model="inputMsg"
              type="textarea"
              :rows="3"
              :disabled="currentHandler === 'ai'"
              :placeholder="currentHandler === 'ai' ? 'AI 托管中（请先接管）' : '输入消息... (Ctrl+Enter 发送)'"
              @keydown.ctrl.enter="sendMessage"
            />
            <div class="input-actions">
              <el-button @click="insertTemplate" :disabled="currentHandler === 'ai'">话术模板</el-button>
              <el-button
                type="primary"
                :disabled="!inputMsg.trim() || currentHandler === 'ai'"
                @click="sendMessage"
              >
                发送 (Ctrl+Enter)
              </el-button>
            </div>
          </div>
        </el-card>
        <el-empty v-else description="请从左侧选择会话" />
      </div>


      <CustomerProfilePanel
        v-if="currentSession"
        :current-session="currentSession"
        :profile-stats="profileStats"
        :profile-tags="profileTags"
        v-model:sop-stage="sopStage"
        @refresh="loadCustomerProfile"
        @save-sop-stage="saveSopStage"
        @send-coupon="sendCoupon"
        @send-product-card="sendProductCard"
        @blacklist="blacklist"
        @close-session="closeSession"
        @ai-summary="aiSummaryCurrent"
        @export-transcript="exportTranscriptCurrent"
        @snooze="snoozeCurrent"
        @set-priority="setPriorityCurrent"
      />
    </div>


    <BlacklistDialog
      v-model="blacklistDialogVisible"
      :blacklist-items="blacklistItems"
      :blacklist-loading="blacklistLoading"
      @reload="loadBlacklist"
      @unblacklist="handleUnblacklist"
    />
  </div>
</template>

<script setup>
// 客户会话列表 · 容器组件:负责组装 composables 与子组件、生命周期编排。
// 数据流/操作逻辑拆分至 ./composables/{useSessionFilters,useSessionList,useSessionActions}.js
// 右栏客户 360° 与黑名单弹窗拆分至 ./components/{CustomerProfilePanel,BlacklistDialog}.vue
import { ref, onMounted, watch } from 'vue'
import { MagicStick, User, ChatLineSquare, Warning } from '@element-plus/icons-vue'
import { getMyAgent, getOnlineAgents } from '@/api/customerService.js'
import { getSessions } from '@/api/customerSession.js'
import { getChannelLabel } from '@/constants/channel'
import { formatTime } from './composables/useSessionFilters'
import { useSessionFilters } from './composables/useSessionFilters'
import { useSessionList } from './composables/useSessionList'
import { useSessionActions } from './composables/useSessionActions'
import CustomerProfilePanel from './components/CustomerProfilePanel.vue'
import BlacklistDialog from './components/BlacklistDialog.vue'

// —— 共享状态:会话数据源 + 输入框内容(列表/操作/聊天多个职责共同读写) ——
const sessions = ref([])
const inputMsg = ref('')

// —— 会话列表与筛选 ——
const { filterStatus, filteredSessions, findSession, upsertSession } = useSessionFilters(sessions)

// —— 会话数据流(消息/标签/快捷回复/AI 建议/坐席 socket) ——
const list = useSessionList({ sessions, inputMsg, findSession, upsertSession })
const {
  currentSession, messages, messagesRef, currentHandler,
  allTags, sessionTags, tagToAdd,
  quickReplies, quickReplySearch,
  aiSuggestions, myAgentId, profileStats, profileTags,
  setActions, setupAgentSocket,
  selectSession, sendMessage,
  loadAllTags, addSessionTag, removeSessionTag,
  loadQuickReplies, onQuickReplySearch, insertQuickReply,
  loadAiSuggestions, useAiSuggestion, loadCustomerProfile
} = list

// —— 操作类逻辑(接管/释放/结束/拉黑/黑名单/坐席状态/SOP/话术) ——
const actions = useSessionActions({ currentSession, currentHandler, inputMsg, myAgentId })
const {
  myStatus, sopStage, handlerSwitching, blacklistedSessionIds,
  blacklistDialogVisible, blacklistItems, blacklistLoading,
  setReload,
  aiSummaryCurrent, exportTranscriptCurrent, snoozeCurrent, setPriorityCurrent,
  showCreateSession, closeSession, handleTakeover, handleRelease,
  saveSopStage, sendCoupon, sendProductCard, blacklist,
  openBlacklistDialog, loadBlacklist, handleUnblacklist, handleStatusChange
} = actions

// 依赖回填:操作成功后刷新会话列表 / 选中会话后加载客户画像(与原实现调用点一致)
setActions({
  loadCustomerProfile
})
setReload(loadSessions)

// 话术模板:填充输入框(原实现保留在容器层)
const insertTemplate = () => {
  inputMsg.value = '您好，请问有什么可以帮您？'
}

// 会话列表加载(容器层保留:多个 composables 通过回调依赖此函数)
const loadSessions = async () => {
  try {
    const res = await getSessions()
    const list = Array.isArray(res) ? res : (res?.list || []);
    sessions.value = list.map((s) => ({
      id: s.id,
      sessionId: s.session_id,
      customerId: s.user_id,
      customerName: s.user_name || '访客',
      channel: s.platform || 'web',
      status: s.status,
      handlerType: s.handler_type || 'ai',
      lastMessage: s.last_message || '',
      lastTime: s.last_message_at || s.created_at,
      createdAt: s.created_at,
      unread: s.unread_count || s.unread || 0,
      tags: s.tags
    }))
  } catch (e) {
    console.error('加载会话列表失败:', e)
    sessions.value = []
  }
};

onMounted(async () => {
  await loadSessions()
  await Promise.all([loadAllTags(), loadQuickReplies()]);
  try {
    const res = await getMyAgent()
    const me = res;
    if (me?.agent_id) {
      myAgentId.value = me.agent_id
      myStatus.value = me.status || 'offline'
      setupAgentSocket()
      return
    }
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
  try {
    const res = await getOnlineAgents()
    const list = res || [];
    const arr = Array.isArray(list) ? list : list.list || []
    if (arr.length > 0) {
      myAgentId.value = arr[0].agent_id
      myStatus.value = arr[0].status || 'offline'
      setupAgentSocket()
    }
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
});

watch(currentSession, (newVal, oldVal) => {
  if (newVal && newVal.id !== oldVal?.id) {
    loadAiSuggestions()
    loadCustomerProfile()
  }
});
</script>

<style scoped lang="scss">
.customer-session-page {
  padding: 20px;
  height: calc(100vh - 100px);
  display: flex;
  flex-direction: column;
}
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
  .header-actions { display: flex; gap: 10px; align-items: center; }
}
.main-content {
  flex: 1;
  display: grid;
  /* 方向10：三栏布局：左 320 / 中 1fr / 右 320 */
  grid-template-columns: 320px 1fr 320px;
  gap: 16px;
  min-height: 0;
}
.session-list, .chat-area {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.session-list :deep(.el-card),
.chat-area :deep(.el-card) {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.session-list :deep(.el-card__body) {
  flex: 1;
  overflow-y: auto;
  padding: 0;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.session-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 15px;
  border-bottom: 1px solid #ebeef5;
  cursor: pointer;
  &:hover { background: #f5f7fa; }
  &.active { background: #ecf5ff; }
  /* C2：已拉黑会话项视觉降级（灰化 + 左侧红线） */
  &.blacklisted {
    opacity: 0.7;
    border-left: 3px solid #F56C6C;
  }
  .session-info { flex: 1; min-width: 0; }
  .session-top { display: flex; justify-content: space-between; }
  .name { font-weight: bold; }
  .time { color: #909399; font-size: 12px; }
  .preview {
    color: #909399;
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin: 2px 0;
  }
  .session-meta {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    margin-top: 2px;
  }
}
.chat-area :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 0;
}
.chat-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  flex-wrap: wrap;
  gap: 8px;
  h3 { margin: 0; }
  .channel { color: #909399; font-size: 12px; }
  .customer-info { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .session-tags { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
  .header-actions { display: flex; gap: 6px; }
  .handler-tag {
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    /* 状态点指示灯：AI 托管=绿灯脉冲 / 人工接管=蓝灯（对应设计文档"顶栏绿灯"） */
    .status-dot {
      display: inline-block;
      width: 8px;
      height: 8px;
      border-radius: 50%;
      margin-right: 2px;
      &.ai {
        background: #10B981;
        box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.6);
        animation: ai-pulse 1.6s ease-out infinite;
      }
      &.human {
        background: #3B82F6;
      }
    }
  }
}
@keyframes ai-pulse {
  0% { box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.6); }
  70% { box-shadow: 0 0 0 6px rgba(16, 185, 129, 0); }
  100% { box-shadow: 0 0 0 0 rgba(16, 185, 129, 0); }
}
.ai-suggestion-bar {
  padding: 10px 20px;
  background: linear-gradient(180deg, #ecf5ff 0%, #f5f7fa 100%);
  border-bottom: 1px solid #ebeef5;
  .ai-bar-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: #4F46E5;
    margin-bottom: 8px;
  }
  .ai-suggestions {
    display: flex;
    gap: 8px;
    overflow-x: auto;
  }
  .ai-suggestion-item {
    min-width: 220px;
    max-width: 320px;
    background: #fff;
    border: 1px solid #dcdfe6;
    border-radius: 6px;
    padding: 8px 10px;
    cursor: pointer;
    transition: all 0.2s;
    &:hover {
      border-color: #4F46E5;
      box-shadow: 0 2px 8px rgba(64, 158, 255, 0.15);
    }
    .ai-text {
      font-size: 13px;
      line-height: 1.4;
      color: #303133;
      margin-bottom: 6px;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
    .ai-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
  }
}
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}
.message {
  display: flex;
  gap: 10px;
  margin-bottom: 15px;
  &.mine {
    flex-direction: row-reverse;
    .bubble { background: #4F46E5; color: white; }
    .sender-tag { color: rgba(255,255,255,0.85); }
  }
}
.bubble {
  max-width: 60%;
  background: #f5f7fa;
  padding: 10px 15px;
  border-radius: 8px;
  .sender-tag {
    font-size: 11px;
    color: #909399;
    margin-bottom: 4px;
    font-weight: 600;
    &.agent { color: #10B981; }
  }
  .content { line-height: 1.5; }
  .time {
    font-size: 11px;
    color: #909399;
    margin-top: 5px;
    text-align: right;
  }
}
.input-area {
  padding: 15px;
  border-top: 1px solid #ebeef5;
  .quick-replies-bar {
    display: flex;
    align-items: center;
    margin-bottom: 10px;
  }
  .input-actions {
    margin-top: 10px;
    text-align: right;
  }
}
.quick-reply-item {
  padding: 4px 0;
  .qr-title {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 4px;
    span { font-weight: 500; }
  }
  .qr-content {
    font-size: 12px;
    color: #909399;
    max-width: 280px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
