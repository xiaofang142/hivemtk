<template>
  <div
    v-if="hasNewMessage && !dismissed"
    class="message-notification"
  >
    <div class="message-content">
      <div class="message-header">
        <el-icon><Bell /></el-icon>
        <span class="message-title">{{ currentMessage.title }}</span>
      </div>
      <div class="message-body">
        <p>{{ currentMessage.content }}</p>
      </div>
      <div class="message-actions">
        <el-button
          type="primary"
          size="small"
          @click="markAsRead"
        >
          {{ $t('知道了') }}
        </el-button>
        <el-button
          type="text"
          size="small"
          @click="dismiss"
        >
          {{ $t('关闭') }}
        </el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Bell } from '@element-plus/icons-vue'
import { notificationApi } from '@/api/notification'

const hasNewMessage = ref(false)
const dismissed = ref(false)
const currentMessage = ref({
  id: '',
  title: '',
  content: ''
})

const DISMISSED_MESSAGES_KEY = 'dismissed_messages'

let checkInterval = null

const getDismissedMessages = () => {
  const dismissed = localStorage.getItem(DISMISSED_MESSAGES_KEY)
  return dismissed ? JSON.parse(dismissed) : []
};

const addToDismissedMessages = (messageId) => {
  const dismissedMessages = getDismissedMessages()
  if (!dismissedMessages.includes(messageId)) {
    dismissedMessages.push(messageId)
    localStorage.setItem(DISMISSED_MESSAGES_KEY, JSON.stringify(dismissedMessages))
  }
};

const isMessageDismissed = (messageId) => {
  return getDismissedMessages().includes(messageId)
};

let pollingStopped = false;

// 飘条 12 秒后自动收起：它是「有新消息」的提示，不是必须处理的弹窗，
// 长时间停在右下角会遮住内容（历史通知中心的未读仍在铃铛上）。
let hideTimer = null
const scheduleAutoHide = () => {
  if (hideTimer) clearTimeout(hideTimer)
  hideTimer = setTimeout(() => { hasNewMessage.value = false }, 12000)
}
const stopAutoHide = () => {
  if (hideTimer) { clearTimeout(hideTimer); hideTimer = null }
}
// 飘条读**本地通知中心**的最新一条，而不是平台侧的 latest。
//
// 两点理由：
//   1. 同源同口径 —— 飘条与通知中心列表读的是同一份数据，不会出现
//      "列表里没有但飘条弹出来"的分裂；
//   2. 平台 latest 是**全局一条**（不分租户、不分人），已读状态记在平台侧；
//      平台消息的已读是按人独立记录的，只有本地通知中心有这个口径。
// 静默窗口（BUG-12，2026-11-04）：飘条按「最近 30 分钟内 + 每 5 分钟最多一次」
// 收敛。T1 实测坏例批量生成时（zero_hit 每条都通知）飘条连弹，虽然 pointer-events
// 已分层，视觉上仍一直压在页面右下、像是"关不掉"。这里只提示真正新到的事，
// 历史积压与连发不再逐条弹（未读仍在铃铛上）。
const QUIET_WINDOW_MS = 30 * 60 * 1000
const MIN_TOAST_INTERVAL_MS = 5 * 60 * 1000
let lastToastAt = 0

const isRecent = (message) => {
  const raw = message?.created_at || message?.createdAt
  if (!raw) return true
  const ts = new Date(raw).getTime()
  if (Number.isNaN(ts)) return true
  return Date.now() - ts <= QUIET_WINDOW_MS
}

const checkNewMessage = async () => {
  if (pollingStopped) return
  try {
    const res = await notificationApi.list({ page: 1, page_size: 1 })
    const message = (res?.list || [])[0]
    if (!message || !message.id || message.id === currentMessage.value.id) return
    if (isMessageDismissed(message.id) || !isRecent(message)) {
      // 旧积压：记成已看过，不弹也不重复检查。
      currentMessage.value = message
      return
    }
    if (Date.now() - lastToastAt < MIN_TOAST_INTERVAL_MS) return
    currentMessage.value = message
    lastToastAt = Date.now()
    hasNewMessage.value = true
    dismissed.value = false
    scheduleAutoHide()
  } catch (error) {
    if (error?.response?.status === 403 || error?.bizCode === 403) {
      pollingStopped = true
      if (checkInterval) { clearInterval(checkInterval); checkInterval = null }
      return
    }
    if (import.meta.env?.DEV) console.warn('获取最新消息失败:', error)
  }
}

const markAsRead = async () => {
  stopAutoHide()
  try {
    if (currentMessage.value.id) {
      await notificationApi.markRead(currentMessage.value.id)
      addToDismissedMessages(currentMessage.value.id);
    }
    hasNewMessage.value = false
    dismissed.value = true
    ElMessage.success(i18n.global.t('消息已标记为已读'))
  } catch (error) {
    console.error('标记消息已读失败:', error)
    ElMessage.error(i18n.global.t('标记消息已读失败'))
  }
};

const dismiss = () => {
  stopAutoHide()
  hasNewMessage.value = false
  dismissed.value = true
};

onMounted(() => {
  checkNewMessage();
  
  checkInterval = setInterval(checkNewMessage, 30000);
})

onUnmounted(() => {
  stopAutoHide()
  if (checkInterval) {
    clearInterval(checkInterval)
  }
})
</script>

<style scoped>
.message-notification {
  position: fixed;
  /* 右下角：原来在 top:80px/right:20px，会正好盖住页面右上角的「刷新」等操作按钮；
     改到右下后又压住了表格分页器的「下一页」（2026-10-31 逐页点验两次踩到）。
     所以这里用 pointer-events 分层：整卡不接管鼠标，只有它自己的按钮可点，
     底层控件（分页/行内操作）始终能点到。 */
  pointer-events: none;
  bottom: 24px;
  right: 20px;
  z-index: 2000;
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  box-shadow: 0 2px 12px 0 rgba(0, 0, 0, 0.1);
  max-width: 320px;
  animation: slideInRight 0.3s ease-out;
}

.message-content {
  padding: 16px;
}

.message-header {
  display: flex;
  align-items: center;
  margin-bottom: 8px;
  font-weight: 500;
  color: #303133;
}

.message-header .el-icon {
  margin-right: 8px;
  color: #4F46E5;
}

.message-title {
  font-size: 14px;
}

.message-body {
  margin-bottom: 12px;
  color: #606266;
  font-size: 13px;
  line-height: 1.5;
}

.message-body p {
  margin: 0;
}

.message-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@keyframes slideInRight {
  from {
    transform: translateX(100%);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}

@keyframes slideOutRight {
  from {
    transform: translateX(0);
    opacity: 1;
  }
  to {
    transform: translateX(100%);
    opacity: 0;
  }
}

.message-notification.hide {
  animation: slideOutRight 0.3s ease-out;
}

/* 允许点击穿透后，卡片自己的按钮必须恢复可点，否则「知道了 / 关闭」失效 */
.message-notification .message-actions :deep(.el-button) {
  pointer-events: auto;
}
</style>
