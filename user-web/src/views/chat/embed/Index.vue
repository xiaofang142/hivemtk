<template>
  <div class="embed-chat-window">
    <ChatWindow
      v-if="loaded"
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

import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import ChatWindow from './ChatWindow.vue'

const route = useRoute()

const channelId = ref('default');
const channelTitle = ref('在线客服')
const widgetColor = ref('#1989fa')
const source = ref('')
const cardId = ref('')
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
  const ref = (route.params.channel_ref || route.params.app_key || '').toString().trim();
  if (ref) {
    channelId.value = ref
  }
  if (route.query.title)
    channelTitle.value = String(route.query.title);
  if (route.query.color) widgetColor.value = String(route.query.color)
  if (route.query.source)
    source.value = String(route.query.source);
  if (route.query.card_id) cardId.value = String(route.query.card_id)
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

