<template>
  <div class="chat-window" :class="{ 'fullscreen': fullscreen }">
    <VisitorHeader
      :title="channelTitle"
      :color="widgetColor"
      :is-online="agentOnline"
      :agent-count="agentCount"
      @close="onClose"
    />

    <div class="offline-banner" v-if="offlineBannerCount > 0">
      <span><svg class="offline-ico" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg> 您有 {{ offlineBannerCount }} 条未读消息</span>
      <button @click="showOfflineList = true">{{ $t('查看') }}</button>
    </div>

    <div class="offline-banner error-banner" v-if="initFailed">
      <span>{{ $t('会话连接失败，请检查网络') }}</span>
      <button @click="openSession(true)">{{ $t('重试') }}</button>
    </div>

    <ChatMessages
      ref="messagesRef"
      :messages="messages"
      :loading="loading"
      :typing="typing"
    />

    <ChatInput
      ref="inputRef"
      :color="widgetColor"
      :sending="sending"
      @send="onSend"
    />

    
    <div v-if="showOfflineList" class="modal-mask" @click.self="showOfflineList = false">
      <div class="modal">
        <div class="modal-header">
          <h3>{{ $t('历史会话') }}</h3>
          <button @click="showOfflineList = false">×</button>
        </div>
        <div class="modal-body">
          <div v-if="offlineSessions.length === 0" class="empty">{{ $t('暂无历史会话') }}</div>
          <div
            v-for="s in offlineSessions"
            :key="s.id"
            class="session-item"
            @click="resumeSession(s)"
          >
            <div class="session-time">{{ formatTime(s.created_at) }}</div>
            <div class="session-preview">{{ s.last_message || '空会话' }}</div>
          </div>
        </div>
      </div>
    </div>

    
    <div v-if="showRating" class="modal-mask" @click.self="showRating = false">
      <div class="modal">
        <div class="modal-header">
          <h3>{{ $t('服务评价') }}</h3>
          <button @click="onClose">×</button>
        </div>
        <div class="modal-body">
          <p>{{ $t('请对本次服务进行评价：') }}</p>
          <div class="rating-stars">
            <span
              v-for="n in 5"
              :key="n"
              class="star"
              :class="{ active: rating >= n }"
              @click="rating = n"
            >★</span>
          </div>
          <textarea v-model="ratingComment" class="reason-input" rows="3" :placeholder="$t('选填，您的宝贵建议...')"></textarea>
          <div class="modal-actions">
            <button class="primary" @click="submitRating">{{ $t('提交') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import VisitorHeader from './components/VisitorHeader.vue'
import ChatMessages from './components/ChatMessages.vue'
import ChatInput from './components/ChatInput.vue'
import { ElMessage } from 'element-plus'
import chatApi from '@/api/chatPublic'
import { ChatSocket } from '@/utils/chatSocket'

const props = defineProps({
  appKey: { type: String, default: '' },
  channelId: { type: String, default: 'default' },
  channelTitle: { type: String, default: '在线客服' },
  widgetColor: { type: String, default: '#1989fa' },
  source: { type: String, default: '' },
  cardId: { type: String, default: '' },
  fullscreen: { type: Boolean, default: false }
})

const emit = defineEmits(['close'])

const effectiveChannelId = computed(() => {
  if (props.channelId && props.channelId !== 'default') return props.channelId
  if (props.appKey) return props.appKey
  return 'default'
});

const visitorId = ref('');
const sessionId = ref('')
const visitorToken = ref('')
const messages = ref([])
const loading = ref(false)
const sending = ref(false)
const typing = ref(false)
const agentOnline = ref(false)
const agentCount = ref(0)
const widgetTitle = ref(props.channelTitle)

const offlineSessions = ref([]);
const showOfflineList = ref(false)
const offlineBannerCount = ref(0)

const initFailed = ref(false);
let transferTimer = null;
const clearTransferTimer = () => {
  if (transferTimer) { clearTimeout(transferTimer); transferTimer = null }
}
const startTransferWaiting = () => {
  clearTransferTimer()
  transferTimer = setTimeout(() => {
    messages.value.push({
      sender_type: 'system',
      content: i18n.global.t('暂无坐席在线，请留言，我们会尽快与您联系'),
      created_at: new Date().toISOString()
    })
  }, 30000)
}

watch(offlineBannerCount, (c) => {
  try {
    if (window.parent && window.parent !== window) {
      window.parent.postMessage({ type: 'mcw-unread', count: c }, '*')
    }
  } catch (e) { console.warn("[request] 后台调用失败(已忽略):", e) }
});

const humanHint = ref(false);

const showRating = ref(false);
const rating = ref(0)
const ratingComment = ref('')

const messagesRef = ref(null);
const inputRef = ref(null)
let socket = null

const initVisitorId = () => {
  let id = localStorage.getItem('chat_visitor_id')
  if (!id) {
    id = 'v_' + Math.random().toString(36).slice(2, 10) + Date.now().toString(36)
    localStorage.setItem('chat_visitor_id', id)
  }
  visitorId.value = id
};

const buildUiMessage = (m) => ({
  id: m.id,
  sender_type: m.sender_type,
  sender_name: m.sender_name || '客服',
  content: m.content,
  content_type: m.content_type,
  card: m.card,
  ai_source: m.ai_source,
  confidence: m.confidence || m.ai_confidence,
  created_at: m.created_at
});

const openSession = async (resume = true) => {
  loading.value = true
  initFailed.value = false
  try {
    const agentRes = await chatApi.getAvailableAgents(effectiveChannelId.value);
    agentCount.value = agentRes?.data?.available || 0
    agentOnline.value = agentCount.value > 0

    const res = await chatApi.openSession({
      channel_id: effectiveChannelId.value,
      visitor_id: visitorId.value,
      resume: resume
    }, effectiveChannelId.value, visitorId.value);
    const data = res?.data || res
    sessionId.value = data.session?.session_id || ''
    visitorToken.value = data.visitor_token || ''
    if (data.welcome_message) {
      messages.value.push({
        sender_type: 'system',
        content: data.welcome_message,
        created_at: new Date().toISOString()
      })
    }
    if (data.session?.is_new_session === false) {
      await loadHistory()
    }
    connectWebSocket();
  } catch (err) {
    console.error('打开会话失败：', err)
    initFailed.value = true
    messages.value.push({
      sender_type: 'system',
      content: i18n.global.t('会话连接失败，请点击右上角重试'),
      created_at: new Date().toISOString()
    })
  } finally {
    loading.value = false
  }
};

const loadHistory = async () => {
  if (!sessionId.value) return
  try {
    const res = await chatApi.getMessages(sessionId.value, 1, 50, effectiveChannelId.value, visitorId.value)
    const list = res?.data?.list || res?.list || []
    const real = list.filter(m => m.sender_type !== 'system')
    if (real.length > 0) {
      messages.value.push(...real.map(buildUiMessage))
    }
  } catch (err) {
    console.warn('加载历史消息失败：', err)
  }
}

const connectWebSocket = () => {
  if (!sessionId.value) return
  // 先关闭旧实例：resumeSession/重试会再次进入本函数，
  // 直接覆盖 socket 变量会泄漏旧 ws、心跳与重连定时器（并继续推送旧会话消息）
  if (socket) { try { socket.close() } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ } socket = null }
  socket = new ChatSocket({
    sessionId: sessionId.value,
    visitorId: visitorId.value,
    visitorToken: visitorToken.value,
    channelId: effectiveChannelId.value,
    baseURL: window.location.origin,
    onMessage: (payload) => {
      typing.value = false
      if (payload.reply || payload.content) {
        messages.value.push(buildUiMessage(payload))
      } else if (payload.system_msg) {
        messages.value.push({
          sender_type: 'system',
          content: payload.system_msg,
          created_at: new Date().toISOString()
        })
      }
    },
    onAgentJoined: (payload) => {
      agentOnline.value = true
      clearTransferTimer()
      messages.value.push({
        sender_type: 'system',
        content: i18n.global.t('客服已接入，正在为您服务'),
        created_at: new Date().toISOString()
      })
    },
    onSessionClosed: (payload) => {
      messages.value.push({
        sender_type: 'system',
        content: i18n.global.t('会话已结束，感谢您的咨询'),
        created_at: new Date().toISOString()
      })
      showRating.value = true
    },
    onOfflineMessages: (payload) => {
      const list = payload?.messages || [];
      if (list.length > 0) {
        messages.value.push(...list.map(buildUiMessage))
        messages.value.push({
          sender_type: 'system',
          content: `您有 ${list.length} 条离线消息`,
          created_at: new Date().toISOString()
        })
        offlineBannerCount.value = list.length
        try { socket && socket.ackDelivered(list.map(m => m.id)) } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
      }
    },
    onAITyping: (payload) => {
      typing.value = !!payload?.typing;
    },
    onConnected: () => {
      initFailed.value = false
    },
    onDisconnected: () => {},
    onError: () => {
      // 连接层失败（含重连耗尽）时亮出横幅，避免聊天窗口静默冻结
      if (!socket || !socket.connected) initFailed.value = true
    }
  })
  socket.connect()
}

const onSend = async (payload) => {
  if (!sessionId.value) return
  const text = typeof payload === 'string' ? payload : (payload?.text || '');
  const attachment = typeof payload === 'object' ? payload?.attachment : null
  const userMsg = {
    id: Date.now(),
    sender_type: 'user',
    sender_name: '我',
    content: text,
    content_type: attachment ? attachment.mediaType : 'text',
    media_url: attachment?.url || '',
    created_at: new Date().toISOString()
  }
  messages.value.push(userMsg)
  sending.value = true
  typing.value = true
  try {
    const body = {
      content: text,
      content_type: attachment ? attachment.mediaType : 'text'
    }
    if (attachment) {
      body.media_url = attachment.url
      body.media_type = attachment.mediaType
      body.media_name = attachment.name
      body.media_size = attachment.size
    }
    const res = await chatApi.sendMessage(sessionId.value, body, effectiveChannelId.value, visitorId.value, visitorToken.value)
    const data = res?.data || res
    if (data.ai_response) {
      typing.value = false
      messages.value.push(buildUiMessage(data.ai_response))
    }
    if (data.ai_cards && data.ai_cards.length) {
      typing.value = false
      data.ai_cards.forEach((c) => {
        messages.value.push({
          id: 'card_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6),
          sender_type: 'ai',
          content_type: 'card',
          card: c,
          created_at: new Date().toISOString()
        })
      })
    }
    if (!data.ai_response && !data.ai_cards?.length && data.transferred) {
      typing.value = false
      messages.value.push({
        sender_type: 'system',
        content: i18n.global.t('正在为您转接人工客服，请稍候...'),
        created_at: new Date().toISOString()
      })
      startTransferWaiting()
    }
  } catch (err) {
    typing.value = false
    messages.value.push({
      sender_type: 'system',
      content: i18n.global.t('消息发送失败：') + (err?.message || err),
      created_at: new Date().toISOString()
    })
  } finally {
    sending.value = false
  }
}

const onTransfer = () => {}

const submitRating = async () => {
  if (rating.value === 0) return
  try {
    await chatApi.rateSession(sessionId.value, rating.value, ratingComment.value, effectiveChannelId.value, visitorId.value)
    showRating.value = false
    rating.value = 0
    ratingComment.value = ''
    ElMessage.success(i18n.global.t('感谢您的评价！'))
  } catch (err) {
    ElMessage.error('评价提交失败：' + (err?.message || ''))
  }
}

const loadOfflineSessions = async () => {
  try {
    const res = await chatApi.getRecentClosedSessions(effectiveChannelId.value, visitorId.value, 10)
    const list = res?.data?.list || res?.list || []
    offlineSessions.value = list
  } catch (err) {
    console.warn('加载历史会话失败：', err)
  }
}

const resumeSession = async (s) => {
  showOfflineList.value = false
  sessionId.value = s.session_id
  messages.value = []
  await loadHistory()
  connectWebSocket()
}

const onClose = async () => {
  if (socket) {
    socket.close()
  }
  if (sessionId.value) {
    try {
      await chatApi.closeSession(sessionId.value, effectiveChannelId.value, visitorId.value)
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }
  emit('close')
}

const formatTime = (t) => {
  if (!t) return ''
  return new Date(t).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

onMounted(() => {
  initVisitorId()
  loadOfflineSessions()
  openSession(true)
})

onUnmounted(() => {
  if (socket) socket.close()
  clearTransferTimer()
})
</script>

<style scoped>
.chat-window {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #fff;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
}
.chat-window.fullscreen {
  height: 100vh;
  border-radius: 0;
  box-shadow: none;
}
.offline-banner {
  background: #fdf6ec;
  color: #b88230;
  padding: 8px 16px;
  font-size: 13px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #faecd8;
  flex-shrink: 0;
}
.offline-banner button {
  background: #F59E0B;
  color: #fff;
  border: none;
  border-radius: 4px;
  padding: 3px 10px;
  font-size: 12px;
  cursor: pointer;
}
.modal-mask {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.modal {
  background: #fff;
  border-radius: 8px;
  width: 320px;
  max-width: 90vw;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
}
.modal-header {
  padding: 12px 16px;
  border-bottom: 1px solid #ebeef5;
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.modal-header h3 {
  margin: 0;
  font-size: 15px;
}
.modal-header button {
  background: transparent;
  border: none;
  font-size: 20px;
  color: #909399;
  cursor: pointer;
  width: 24px;
  height: 24px;
}
.modal-body {
  padding: 16px;
  overflow-y: auto;
  flex: 1;
}
.modal-body p {
  margin: 0 0 12px;
  font-size: 13px;
  color: #606266;
}
.modal-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 12px;
}
.modal-actions button {
  border: 1px solid #dcdfe6;
  background: #fff;
  border-radius: 4px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 13px;
}
.modal-actions button.primary {
  background: #1989fa;
  color: #fff;
  border-color: #1989fa;
}
.reason-input {
  width: 100%;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 8px 10px;
  font-size: 13px;
  font-family: inherit;
  resize: vertical;
  box-sizing: border-box;
}
.session-item {
  padding: 10px 0;
  border-bottom: 1px solid #f5f5f5;
  cursor: pointer;
}
.session-item:hover {
  background: #fafafa;
}
.session-time {
  font-size: 12px;
  color: #909399;
}
.session-preview {
  font-size: 13px;
  color: #303133;
  margin-top: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.empty {
  text-align: center;
  color: #909399;
  padding: 30px 0;
  font-size: 13px;
}
.rating-stars {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
  justify-content: center;
}
.star {
  font-size: 28px;
  color: #dcdfe6;
  cursor: pointer;
  transition: color 0.2s;
}
.star.active {
  color: #f7ba2a;
}
</style>
