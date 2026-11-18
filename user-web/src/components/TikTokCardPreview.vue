<template>
  <div class="tiktok-card-preview">
    <div class="card-container">
      <div class="card-header">
        <div class="avatar" />
        <div class="user-info">
          <div class="username">
            @{{ cardData.username || 'username' }}
          </div>
          <div class="time">
            {{ cardData.time || '2小时前' }}
          </div>
        </div>
        <div class="more-icon">
          ⋯
        </div>
      </div>
      
      <div class="card-content">
        <h3>{{ cardData.title || '标题' }}</h3>
        <p>{{ cardData.content || '这里是内容预览...' }}</p>
        
        <div
          v-if="cardData.media"
          class="media-container"
        >
          <img
            :src="cardData.media"
            alt="Media"
            class="media"
          >
        </div>
      </div>
      
      <div class="card-footer">
        <div class="action">
          <span class="icon">♡</span>
          <span>{{ cardData.likes || 0 }}</span>
        </div>
        <div class="action">
          <el-icon class="icon">
            <ChatDotRound />
          </el-icon>
          <span>{{ cardData.comments || 0 }}</span>
        </div>
        <div class="action">
          <span class="icon">↻</span>
          <span>{{ cardData.shares || 0 }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { defineProps } from 'vue'

const _props = defineProps({
  cardData: {
    type: Object,
    default: () => ({
      username: 'username',
      time: '2小时前',
      title: i18n.global.t('标题'),
      content: i18n.global.t('这里是内容预览...'),
      media: '',
      likes: 0,
      comments: 0,
      shares: 0
    })
  }
})
</script>

<style scoped>
.tiktok-card-preview {
  padding: 16px;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
}

.card-container {
  background: white;
  border-radius: 8px;
  box-shadow: 0 2px 10px rgba(0,0,0,0.1);
  overflow: hidden;
  max-width: 400px;
  margin: 0 auto;
}

.card-header {
  display: flex;
  align-items: center;
  padding: 12px 16px;
}

.avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: #ddd;
  margin-right: 12px;
}

.user-info {
  flex: 1;
}

.username {
  font-weight: bold;
  color: #000;
  margin-bottom: 2px;
}

.time {
  font-size: 12px;
  color: #666;
}

.more-icon {
  font-size: 20px;
  color: #666;
}

.card-content {
  padding: 0 16px 12px;
}

.card-content h3 {
  margin: 0 0 8px;
  font-size: 16px;
  font-weight: 600;
  color: #000;
}

.card-content p {
  margin: 0 0 12px;
  font-size: 14px;
  color: #222;
  line-height: 1.4;
}

.media-container {
  margin-top: 8px;
}

.media {
  width: 100%;
  border-radius: 8px;
  max-height: 200px;
  object-fit: cover;
}

.card-footer {
  display: flex;
  padding: 12px 16px;
  border-top: 1px solid #eee;
}

.action {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: #666;
  font-size: 14px;
}

.icon {
  font-size: 16px;
}
</style>
