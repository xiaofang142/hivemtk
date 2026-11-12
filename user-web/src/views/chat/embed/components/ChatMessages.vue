<template>
  <div
    ref="listRef"
    class="chat-messages"
  >
    <div
      v-if="loading"
      class="loading"
    >
      加载中...
    </div>
    <div
      v-else-if="messages.length === 0"
      class="empty"
    >
      <div class="empty-icon">
        <svg
          viewBox="0 0 24 24"
          width="48"
          height="48"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          stroke-linejoin="round"
        ><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" /></svg>
      </div>
      <p>{{ $t('开始对话吧') }}</p>
    </div>

    <template v-else>
      <div
        v-for="(msg, idx) in messages"
        :key="msg.id || idx"
        class="message-row"
        :class="messageClass(msg)"
      >
        <div
          v-if="msg.content_type === 'card' && msg.card"
          class="bubble-wrap card-wrap"
        >
          <RichCard
            :card="msg.card"
            @action="onCardAction"
          />
        </div>
        <template v-else>
          <div
            v-if="msg.sender_type !== 'system'"
            class="avatar"
            :style="avatarStyle(msg)"
          >
            <span class="avatar-text">{{ avatarText(msg) }}</span>
          </div>
          <div class="bubble-wrap">
            <div
              v-if="showName(msg)"
              class="sender-name"
            >
              {{ displayName(msg) }}
            </div>
            <div
              class="bubble"
              :class="bubbleClass(msg)"
            >
              <!-- 002-multi-type-messages：按 content_type 渲染媒体（图/视频/语音/文件），
                   媒体在上、文字在下；RichCard 已在上方分支处理。 -->
              <div
                v-if="msg.content_type === 'image' && msg.media_url"
                class="media-block"
              >
                <img
                  :src="msg.media_url"
                  class="media-image"
                  alt=""
                  @error="onMediaError(msg)"
                >
              </div>
              <!-- eslint-disable-next-line vuejs-accessibility/media-has-caption -- 访客上传的 UGC 音视频没有字幕轨，平台侧无法替上传者补 -->
              <video
                v-else-if="msg.content_type === 'video' && msg.media_url"
                :src="msg.media_url"
                controls
                preload="metadata"
                class="media-video"
              />
              <!-- eslint-disable-next-line vuejs-accessibility/media-has-caption -- 同上：UGC 语音消息无字幕源 -->
              <audio
                v-else-if="msg.content_type === 'audio' && msg.media_url"
                :src="msg.media_url"
                controls
                preload="metadata"
                class="media-audio"
              />
              <div
                v-else-if="msg.content_type === 'file' && msg.media_url"
                class="media-file"
              >
                <a
                  :href="msg.media_url"
                  target="_blank"
                  rel="noopener"
                  class="media-file-link"
                >{{ mediaFileName(msg) }}</a>
              </div>
              <div
                class="bubble-content"
                v-html="formatContent(msg.content)"
              />
              <!-- T2 引用溯源：AI 消息气泡下方的参考来源行 -->
              <CitationList
                v-if="isAI(msg) && msg.citations?.length"
                :citations="msg.citations"
                @open="openCitation"
              />
            </div>
            <div class="time">
              {{ formatTime(msg.created_at) }}
            </div>
          </div>
        </template>
      </div>

      <div
        v-if="typing"
        class="message-row left"
      >
        <div
          class="avatar"
          :style="avatarStyle({ sender_type: 'ai' })"
        >
          <!-- 32px 圆头像只放一个字符，和上面 avatarText(msg) 的口径一致；
               整词 $t('客服') 在非中文语种是 "Customer Service"，会从圆里铺到气泡上。 -->
          <span class="avatar-text">{{ $t('客服').slice(0, 1) }}</span>
        </div>
        <div class="bubble-wrap">
          <div class="bubble typing">
            <span class="dot" /><span class="dot" /><span class="dot" />
          </div>
        </div>
      </div>
    </template>

    <!-- T2 引用详情浮窗：FAQ 问/答或文档片段 -->
    <Teleport to="body">
      <!-- eslint-disable-next-line vuejs-accessibility/click-events-have-key-events, vuejs-accessibility/no-static-element-interactions -- 遮罩点击关闭；键盘用户可用浮窗内的关闭按钮 -->
      <div
        v-if="citationDetail"
        class="citation-overlay"
        @click.self="citationDetail = null"
      >
        <div class="citation-detail">
          <div class="citation-detail-head">
            <span>{{ citationDetail.type === 'chunk' ? $t('citation.doc') : $t('citation.faqTitle') }}</span>
            <button
              class="citation-close"
              type="button"
              @click="citationDetail = null"
            >
              ×
            </button>
          </div>
          <template v-if="citationDetail.type === 'faq' && citationDetail.faq">
            <div class="citation-q-block">
              <strong>Q</strong>{{ citationDetail.faq.question }}
            </div>
            <div class="citation-a-block">
              <strong>A</strong>{{ citationDetail.faq.answer }}
            </div>
          </template>
          <template v-else-if="citationDetail.type === 'chunk'">
            <div class="citation-a-block">
              <strong>{{ chunkTitle(citationDetail) }}</strong>{{ citationDetail.chunk?.content || citationDetail.content || '' }}
            </div>
          </template>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, watch, nextTick, onMounted } from 'vue'
import DOMPurify from 'dompurify';
import RichCard from './RichCard.vue'
import CitationList from './citation/CitationList.vue'

const props = defineProps({
  messages: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  typing: { type: Boolean, default: false }
})

const listRef = ref(null)

const messageClass = (msg) => {
  if (msg.sender_type === 'user') return 'right'
  if (msg.sender_type === 'system') return 'center'
  return 'left'
}

const bubbleClass = (msg) => {
  if (msg.sender_type === 'user') return 'user'
  if (msg.sender_type === 'system') return 'system'
  if (msg.sender_type === 'agent') return 'agent'
  return 'ai'
}

// T2：引用行只挂在 AI 消息上
const isAI = (msg) => msg?.sender_type === 'ai'

// T2：点击引用卡片 → 浮窗呈现引用详情（FAQ 问/答或文档片段）
const citationDetail = ref(null)
const openCitation = (c) => {
  citationDetail.value = c || null
}

const displayName = (msg) => {
  if (msg.sender_type === 'user' || msg.sender_type === 'system') return ''
  if (msg.sender_type === 'ai') return '客服'
  return msg.sender_name || '客服'
};

const avatarBg = (msg) => {
  if (msg.sender_type === 'user') return '#1989fa'
  if (msg.sender_type === 'agent') return '#10B981'
  return '#909399';
};

const avatarText = (msg) => {
  if (msg.sender_type === 'user') {
    const name = msg.sender_name || '我'
    return name.slice(0, 1).toUpperCase()
  }
  if (msg.sender_type === 'agent') {
    const name = msg.sender_name || '客'
    return name.slice(0, 1)
  }
  return '客'
};

const avatarStyle = (msg) => ({ background: avatarBg(msg) })

const onCardAction = (btn) => {
  if (btn && btn.action)
    { /* no-op */ }
};

const showName = (msg) => {
  if (msg.sender_type === 'user' || msg.sender_type === 'system') return false
  return true
};

const formatTime = (t) => {
  if (!t) return ''
  const d = new Date(t)
  const now = new Date()
  const isSameDay = d.toDateString() === now.toDateString()
  if (isSameDay) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

const formatContent = (text) => {
  if (!text) return ''
  const escaped = String(text)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
  const html = escaped.replace(/\n/g, '<br>')
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } });
};

// ---- 002-multi-type-messages：媒体渲染辅助 ----
const mediaFileName = (msg) => {
  try {
    return decodeURIComponent((msg.media_url || '').split('?')[0].split('/').pop() || i18n.global.t('文件'))
  } catch {
    return i18n.global.t('文件')
  }
}
const onMediaError = (msg) => {
  // 图片加载失败退化为正文占位（bubble-content 仍显示 content）
  msg.media_url = ''
}

const scrollToBottom = async () => {
  await nextTick()
  if (listRef.value) {
    listRef.value.scrollTop = listRef.value.scrollHeight
  }
}

watch(() => props.messages.length, scrollToBottom)
watch(() => props.typing, scrollToBottom)
onMounted(scrollToBottom)

defineExpose({ scrollToBottom })
</script>

<style scoped>
.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  background: #fafafa;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.loading,
.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #909399;
  font-size: 13px;
}
.empty-icon {
  font-size: 36px;
  margin-bottom: 8px;
}
.message-row {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.message-row.right {
  flex-direction: row-reverse;
}
.message-row.center {
  justify-content: center;
}
.avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #f0f0f0;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.avatar-emoji {
  font-size: 18px;
}
.bubble-wrap {
  max-width: 70%;
  display: flex;
  flex-direction: column;
}
.bubble-wrap.card-wrap {
  max-width: 82%;
}
.message-row.right .bubble-wrap {
  align-items: flex-end;
}
.sender-name {
  font-size: 12px;
  color: #909399;
  margin-bottom: 4px;
  padding: 0 4px;
}
.bubble {
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 14px;
  line-height: 1.5;
  word-wrap: break-word;
  word-break: break-word;
  max-width: 100%;
}
.bubble-content {
  white-space: pre-wrap;
}
/* 002-multi-type-messages：媒体块 */
.media-block {
  margin-bottom: 4px;
}
.media-image {
  max-width: 240px;
  max-height: 200px;
  border-radius: 6px;
  display: block;
  cursor: zoom-in;
}
.media-video {
  max-width: 260px;
  max-height: 200px;
  border-radius: 6px;
  display: block;
  margin-bottom: 4px;
}
.media-audio {
  max-width: 240px;
  display: block;
  margin-bottom: 4px;
}
.media-file {
  margin-bottom: 4px;
}
.media-file-link {
  color: #1989fa;
  text-decoration: none;
  word-break: break-all;
  font-size: 13px;
}
.bubble.user .media-file-link {
  color: #fff;
}
.bubble.user {
  background: #1989fa;
  color: #fff;
  border-bottom-right-radius: 2px;
}
.bubble.ai {
  background: #fff;
  color: #303133;
  border: 1px solid #ebeef5;
  border-bottom-left-radius: 2px;
}
.bubble.agent {
  background: #fff7e6;
  color: #303133;
  border: 1px solid #ffe7ba;
  border-bottom-left-radius: 2px;
}
.bubble.system {
  background: transparent;
  color: #909399;
  font-size: 12px;
  padding: 2px 8px;
}
.time {
  font-size: 11px;
  color: #c0c4cc;
  margin-top: 2px;
  padding: 0 4px;
}
.bubble.typing {
  background: #fff;
  border: 1px solid #ebeef5;
  padding: 12px 16px;
  display: flex;
  gap: 4px;
  align-items: center;
}
.bubble.typing .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #c0c4cc;
  animation: typingBounce 1.4s infinite;
}
.bubble.typing .dot:nth-child(2) { animation-delay: 0.2s; }
.bubble.typing .dot:nth-child(3) { animation-delay: 0.4s; }
@keyframes typingBounce {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.4; }
  30% { transform: translateY(-4px); opacity: 1; }
}

/* T2 引用详情浮窗（Teleport 到 body，scoped 不生效，用独立类名） */
.citation-overlay {
  position: fixed; inset: 0; z-index: 3000;
  background: rgba(0,0,0,.35);
  display: flex; align-items: center; justify-content: center;
}
.citation-detail {
  max-width: 480px; width: 88%; max-height: 70vh; overflow: auto;
  background: #fff; border-radius: 10px; padding: 14px 16px;
  box-shadow: 0 8px 30px rgba(0,0,0,.18);
  font-size: 14px; color: #303133;
}
.citation-detail-head {
  display: flex; justify-content: space-between; align-items: center;
  font-weight: 600; margin-bottom: 10px;
}
.citation-close {
  border: none; background: none; font-size: 20px; line-height: 1;
  color: #909399; cursor: pointer;
}
.citation-q-block, .citation-a-block {
  margin: 8px 0; padding: 8px 10px; border-radius: 6px;
  background: #f5f7fa; line-height: 1.6; white-space: pre-wrap;
}
.citation-q-block strong, .citation-a-block strong {
  display: inline-block; margin-right: 8px; color: #1989fa;
}
</style>
