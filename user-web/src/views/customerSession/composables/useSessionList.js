// 客户会话 · 会话列表页核心数据流:选中会话 / 收发消息 / 标签 / 快捷回复 / AI 建议 / 坐席 socket 推送
// 由 views/customerSession/List.vue 原样迁出(零行为变更拆分)
// 共享状态(sessions/inputMsg)与跨域动作由容器注入,保持与原实现相同的语义与调用顺序
// 注意:agentSocketInst 为模块级单例,与原实现一致(该页面路由上单实例)
import { ref, computed, nextTick, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import i18n from '@/i18n'
import {
  getSessionMessages,
  sendMessage as sendMsg,
  getCustomerStats,
  getCustomerTags
} from '@/api/customerSession.js'
import {
  getSessionTags,
  getQuickReplies,
  getQuickReplyCategories,
  getAISuggestions,
  useAISuggestion
} from '@/api/customerService.js'
import AgentSocket from '@/utils/agentSocket'
import { formatTime, mapSession } from './useSessionFilters'

export function useSessionList({ sessions, inputMsg, findSession, upsertSession }) {
  const currentSession = ref(null)
  const messages = ref([])
  const messagesRef = ref()

  const currentHandler = computed(() => {
    return currentSession.value?.handlerType === 'human' ? 'human' : 'ai'
  });

  const allTags = ref([]);
  const sessionTags = ref([]);
  const tagToAdd = ref(null)

  const quickReplies = ref([]);
  const allQuickReplies = ref([]);
  const quickReplySearch = ref('')

  const aiSuggestions = ref([]);

  const myAgentId = ref(null)

  // 客户画像状态(由 selectSession/watch 联动加载,UI 在 CustomerProfilePanel)
  const profileStats = ref({ messageCount: 0, sessionCount: 0, aiReplyCount: 0 });
  const profileTags = ref([])
  // 由容器注入:选中会话后加载客户画像(避免循环依赖,语义不变)
  let loadCustomerProfileHook = () => {}
  const setActions = ({ loadCustomerProfile: lcp }) => {
    if (lcp) loadCustomerProfileHook = lcp
  }

  const mapMessage = (m) => {    const st = m.SenderType ?? m.sender_type
    const isVisitor = st === 'visitor' || st === 'user'
    return {
      id: m.ID ?? m.id,
      direction: isVisitor ? 'in' : 'out',
      senderType: st,
      from: m.SenderName ?? (st === 'ai' ? 'AI 助手' : isVisitor ? '访客' : '客服'),
      content: m.Content ?? m.content,
      createdAt: formatTime(m.CreatedAt ?? m.created_at)
    }
  }

  let agentSocketInst = null;

  const onAgentNewSession = (payload) => {
    const session = mapSession(payload.session)
    if (!session) return
    const isCurrent = currentSession.value &&
      (currentSession.value.sessionId === session.sessionId || String(currentSession.value.id) === String(session.id))
    upsertSession(session)
    if (!isCurrent) ElMessage.info(`新会话接入：${session.customerName || '访客'}`)
  }

  const onAgentNewMessage = (payload) => {
    const sid = payload.session_id
    const session = findSession(sid)
    const raw = payload.message
    const msg = raw ? mapMessage(raw) : null
    if (session) {
      session.lastMessage = msg ? msg.content : (payload.content || session.lastMessage)
      session.lastTime = msg ? (raw.CreatedAt ?? raw.created_at) : new Date().toISOString()
    }
    const isCurrent = currentSession.value &&
      (currentSession.value.sessionId === sid || String(currentSession.value.id) === String(sid))
    if (isCurrent && msg) {
      messages.value.push(msg)
      nextTick(scrollToBottom)
    } else if (session) {
      session.unread = (session.unread || 0) + 1
    }
  }

  const onAgentSessionUpdate = (payload) => {
    const session = findSession(payload.session_id)
    if (!session) return
    if (payload.handler_type) session.handlerType = payload.handler_type
    if (payload.handlerType) session.handlerType = payload.handlerType
    if (payload.status) session.status = payload.status
    if (payload.transferred) session.transferred = payload.transferred
    if (currentSession.value && (currentSession.value.sessionId === payload.session_id || String(currentSession.value.id) === String(payload.session_id))) {
      if (payload.handler_type) currentSession.value.handlerType = payload.handler_type
      if (payload.handlerType) currentSession.value.handlerType = payload.handlerType
      if (payload.status) currentSession.value.status = payload.status
    }
  };

  const onAgentAISuggestion = () => {
    if (currentSession.value) loadAiSuggestions()
  }

  const setupAgentSocket = () => {
    if (!myAgentId.value || agentSocketInst) return
    agentSocketInst = new AgentSocket(myAgentId.value, undefined, {
      onNewSession: onAgentNewSession,
      onNewMessage: onAgentNewMessage,
      onSessionUpdate: onAgentSessionUpdate,
      onAISuggestion: onAgentAISuggestion,
      onError: (e) => { console.warn('[agentSocket]', e) }
    })
    agentSocketInst.connect()
  }

  const selectSession = async (session) => {
    currentSession.value = session
    try {
      const res = await getSessionMessages(session.id)
      const list = Array.isArray(res) ? res : (res?.list || [])
      messages.value = list.map((m) => ({
        id: m.id,
        direction: m.sender_type === 'user' ? 'in' : 'out',
        senderType: m.sender_type,
        from: m.sender_name || (m.sender_type === 'user' ? '访客' : m.sender_type === 'ai' ? 'AI 助手' : '客服'),
        content: m.content,
        createdAt: formatTime(m.created_at)
      }))
      await Promise.all([loadSessionTags(), loadAiSuggestions(), loadCustomerProfileHook()]);
      await nextTick()
      scrollToBottom()
    } catch (e) {
      console.error('加载会话消息失败:', e)
      ElMessage.error(i18n.global.t('加载会话消息失败'))
    }
  }

  const sendMessage = async () => {
    if (!inputMsg.value.trim() || !currentSession.value) return
    if (currentHandler.value === 'ai') {
      ElMessage.warning('AI 托管中，请先接管会话')
      return
    }
    const content = inputMsg.value.trim()
    inputMsg.value = ''
    try {
      await sendMsg({
        sessionId: currentSession.value.id,
        content,
        sender_type: 'agent',
        sender_name: '客服',
        sender_id: String(myAgentId.value || '')
      })
      messages.value.push({
        id: Date.now(),
        direction: 'out',
        senderType: 'agent',
        from: 'me',
        content,
        createdAt: new Date().toLocaleTimeString()
      })
      await nextTick()
      scrollToBottom()
    } catch (e) {
      inputMsg.value = content;
      ElMessage.error('发送失败：' + (e?.message || ''))
    }
  }

  const scrollToBottom = () => {
    if (messagesRef.value) {
      messagesRef.value.scrollTop = messagesRef.value.scrollHeight
    }
  }

  const loadAllTags = async () => {
    try {
      const res = await getSessionTags()
      const data = res || [];
      allTags.value = Array.isArray(data) ? data : data.list || []
    } catch (e) {
      allTags.value = []
    }
  };

  const loadSessionTags = () => {
    if (!currentSession.value) {
      sessionTags.value = []
      return
    }
    const raw = currentSession.value.tags
    let ids = []
    try {
      ids = typeof raw === 'string' ? JSON.parse(raw || '[]') : raw || []
    } catch {
      ids = []
    }
    sessionTags.value = (allTags.value || []).filter((t) => ids.includes(t.id))
  };

  const addSessionTag = async (tagId) => {
    if (!tagId || !currentSession.value) return
    const exists = sessionTags.value.find((t) => t.id === tagId)
    if (exists) {
      ElMessage.warning(i18n.global.t('标签已存在'))
      return
    }
    const tag = allTags.value.find((t) => t.id === tagId)
    if (tag) {
      sessionTags.value.push(tag)
      await persistSessionTags()
      ElMessage.success(i18n.global.t('标签已添加'))
    }
    tagToAdd.value = null
  }

  const removeSessionTag = async (tag) => {
    sessionTags.value = sessionTags.value.filter((t) => t.id !== tag.id)
    await persistSessionTags()
  }

  const persistSessionTags = async () => {
    const tagService = await import('@/api/customerService.js');
    const ids = sessionTags.value.map((t) => t.id)
    try {
      await tagService.tagSession(currentSession.value.id, { tags: ids })
    } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }

  const loadQuickReplies = async () => {
    try {
      const [rRes] = await Promise.all([
        getQuickReplies().catch(() => ({ data: [] })),
        getQuickReplyCategories().catch(() => ({ data: [] }))
      ])
      const data = Array.isArray(rRes) ? rRes : (rRes?.data || rRes?.list || [])
      allQuickReplies.value = Array.isArray(data) ? data : data.list || []
      quickReplies.value = allQuickReplies.value
    } catch (e) {
      allQuickReplies.value = []
      quickReplies.value = []
    }
  };

  const onQuickReplySearch = () => {
    const kw = quickReplySearch.value.trim().toLowerCase()
    if (!kw) {
      quickReplies.value = allQuickReplies.value
      return
    }
    quickReplies.value = allQuickReplies.value.filter(
      (r) =>
        r.title?.toLowerCase().includes(kw) ||
        r.content?.toLowerCase().includes(kw) ||
        r.category?.toLowerCase().includes(kw)
    );
  }

  const insertQuickReply = (reply) => {
    if (!reply) return
    inputMsg.value = reply.content
    ElMessage.success(`已插入：${reply.title}`)
  }

  const loadAiSuggestions = async () => {
    if (!currentSession.value) {
      aiSuggestions.value = []
      return
    }
    try {
      const res = await getAISuggestions(currentSession.value.sessionId || currentSession.value.id)
      const data = res || [];
      aiSuggestions.value = Array.isArray(data) ? data : data.list || []
    } catch (e) {
      aiSuggestions.value = []
    }
  };

  const useAiSuggestion = async (suggestion) => {
    if (!suggestion || suggestion.is_used) return
    try {
      if (useAISuggestion) {
        await useAISuggestion(suggestion.id)
      }
      inputMsg.value = suggestion.suggestion;
      suggestion.is_used = true
      ElMessage.success(i18n.global.t('已采纳，可直接发送'))
    } catch (e) {
      ElMessage.error(i18n.global.t('采纳失败'))
    }
  }

  const loadCustomerProfile = async () => {
    if (!currentSession.value) return
    const uid = currentSession.value.customerId || currentSession.value.sessionId
    try {
      const [statsRes, tagsRes] = await Promise.all([
        getCustomerStats(uid).catch(() => null),
        getCustomerTags(uid).catch(() => null)
      ])
      profileStats.value = statsRes || profileStats.value
      const tagsData = Array.isArray(tagsRes) ? tagsRes : (tagsRes?.list || [])
      profileTags.value = tagsData
    } catch (e) {
      console.warn('[profile] load failed:', e)
    }
  };

  onUnmounted(() => {
    if (agentSocketInst) { agentSocketInst.close(); agentSocketInst = null }
  })

  return {
    currentSession,
    messages,
    messagesRef,
    currentHandler,
    allTags,
    sessionTags,
    tagToAdd,
    quickReplies,
    quickReplySearch,
    aiSuggestions,
    myAgentId,
    profileStats,
    profileTags,
    setActions,    setupAgentSocket,
    selectSession,
    sendMessage,
    scrollToBottom,
    loadAllTags,
    loadSessionTags,
    loadQuickReplies,
    onQuickReplySearch,
    insertQuickReply,
    loadAiSuggestions,
    useAiSuggestion,
    loadCustomerProfile
  }
}
