<template>
  <div class="embed-chat-window">
    <ChatWindow
      v-if="loaded"
      :key="widgetKey"
      :channel-id="channelId"
      :channel-title="channelTitle"
      :widget-color="widgetColor"
      :source="source"
      :card-id="cardId"
      fullscreen
      @close="onClose"
    />
    <div
      v-else-if="closed"
      class="closed-container"
    >
      <p>{{ $t('会话已结束') }}</p>
    </div>
    <div
      v-else
      class="loading-container"
    >
      <div class="loading-spinner" />
      <p>正在加载...</p>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { computed, ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import ChatWindow from './ChatWindow.vue'

const route = useRoute()

// 路由参数/query 直接 computed 响应式读取：同文档 hash 导航（/chat/embed/A → /chat/embed/B）
// 组件复用时 onMounted 不再重跑，旧实现会把上一渠道的 channelId 残留进新页面
// （2026-11-10 J 轮 r5 实证：/chat/embed/uit 显示「渠道未配置或不可用: default」）。
const channelId = computed(() => {
  const param = (route.params.channel_ref || route.params.app_key || '').toString().trim();
  return param || 'default'
})
const channelTitle = computed(() => String(route.query.title || '在线客服'))
const widgetColor = computed(() => String(route.query.color || '#1989fa'))
const source = computed(() => String(route.query.source || ''))
const cardId = computed(() => String(route.query.card_id || ''))
// 渠道标识/配置任一变化整体重挂 ChatWindow：openSession 只在挂载时触发，
// key 变化才能让新渠道拿到干净会话，而不是复用旧渠道的会话态。
const widgetKey = computed(() => [channelId.value, channelTitle.value, widgetColor.value, source.value, cardId.value].join('|'))

const loaded = ref(false)
const closed = ref(false)

const onClose = () => {
  try {
    window.parent.postMessage({ type: 'chat-widget-close' }, '*')
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  // 嵌入场景由宿主页面收起 iframe；独立打开时展示已结束态，不再跳到 404 页
  closed.value = true
  loaded.value = false
}

onMounted(() => {
  loaded.value = true
})
</script>

<style>
html, body, #app {
  margin: 0;
  padding: 0;
  height: 100%;
  overflow: hidden;
  background: #fff;
}
</style>

<style scoped>
.embed-chat-window {
  height: 100vh;
  background: #fff;
}
.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #909399;
  font-size: 14px;
}
.closed-container {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #909399;
  font-size: 14px;
}
.loading-spinner {
  width: 32px;
  height: 32px;
  border: 3px solid #f0f0f0;
  border-top-color: #1989fa;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  margin-bottom: 12px;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>

