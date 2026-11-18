<template>
  <div class="xiaohongshu-card-preview">
    <div class="preview-header">
      <h3>{{ $t('小红书卡片预览') }}</h3>
      <el-switch
        v-model="isPreviewMode"
        active-text="预览模式"
        inactive-text="编辑模式"
        @change="handleModeChange"
      />
    </div>
    
    <div
      class="preview-container"
      :class="{ 'preview-mode': isPreviewMode }"
    >
      <div class="phone-container">
        <div class="phone-frame">
          <div class="phone-status-bar">
            <div class="status-left">
              <span class="time">9:41</span>
            </div>
            <div class="status-center">
              <span class="chat-title">{{ $t('小红书') }}</span>
            </div>
            <div class="status-right">
              <el-icon><Connection /></el-icon>
              <el-icon><Link /></el-icon>
              <el-icon><CircleCheckFilled /></el-icon>
            </div>
          </div>
          
          
          <div class="chat-container">
            <div class="message-container">
              <div class="message-bubble">
                <div class="card-content">
                  <div
                    v-if="cardData.image_url"
                    class="card-image"
                  >
                    <el-image 
                      :src="cardData.image_url" 
                      fit="cover"
                      :preview-src-list="[cardData.image_url]"
                    >
                      <template #error>
                        <div class="image-placeholder">
                          <el-icon><Picture /></el-icon>
                          <span>{{ $t('图片加载失败') }}</span>
                        </div>
                      </template>
                    </el-image>
                  </div>
                  
                  
                  <div class="card-text">
                    <div
                      v-if="cardData.title"
                      class="card-title"
                    >
                      {{ cardData.title }}
                    </div>
                    
                    
                    <div
                      v-if="cardData.description"
                      class="card-description"
                    >
                      {{ cardData.description }}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import '@/i18n'

import { ref, watch } from 'vue'
import { Picture, Connection, Link, CircleCheckFilled } from '@element-plus/icons-vue'
import 'element-plus'

const props = defineProps({
  cardData: {
    type: Object,
    default: () => ({})
  }
});

defineEmits(['view']);

const isPreviewMode = ref(false);

const handleModeChange = () => {
};

watch(() => props.cardData, () => {
}, { deep: true });
</script>

<style scoped>
.xiaohongshu-card-preview {
  width: 100%;
  max-width: 400px;
  margin: 0 auto;
}

.preview-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
  padding: 0 10px;
}

.preview-header h3 {
  margin: 0;
  font-size: 16px;
}

.preview-container {
  transition: all 0.3s ease;
}

.preview-container.preview-mode {
  transform: scale(0.9);
  box-shadow: 0 10px 20px rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  overflow: hidden;
}

/* 手机容器样式 */
.phone-container {
  width: 100%;
  max-width: 375px;
  margin: 0 auto;
  background: #f5f5f5;
  border-radius: 36px;
  padding: 12px;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.2);
  position: relative;
}

.phone-frame {
  width: 100%;
  height: 667px;
  background: #fff;
  border-radius: 24px;
  overflow: hidden;
  position: relative;
  display: flex;
  flex-direction: column;
}

/* 手机状态栏 */
.phone-status-bar {
  height: 44px;
  background: #fff;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid #f0f0f0;
}

.status-left .time {
  font-weight: 600;
  font-size: 14px;
}

.status-center .chat-title {
  font-weight: 600;
  font-size: 16px;
  color: #ff2442; /* 小红书品牌色 */
}

.status-right {
  display: flex;
  align-items: center;
  gap: 5px;
}

.status-right .el-icon {
  font-size: 14px;
}

/* 聊天容器 */
.chat-container {
  flex: 1;
  background: #f5f5f5;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* 消息容器 */
.message-container {
  padding: 15px;
  display: flex;
  justify-content: flex-start;
}

.message-bubble {
  width: 100%;
  background: #fff;
  border-radius: 8px;
  padding: 12px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

/* 卡片内容样式 */
.card-content {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  height: 90px;
}

.card-title {
  font-size: 13px;
  font-weight: bold;
  margin-bottom: 0;
  line-height: 1.3;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.card-image {
  margin-bottom: 0;
  border-radius: 4px;
  overflow: hidden;
  flex-shrink: 0;
  width: 60px;
  height: 60px;
}

.card-image .el-image {
  width: 100%;
  height: 100%;
  display: block;
}

.image-placeholder {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  height: 60px;
  background-color: #f5f7fa;
  color: #909399;
}

.image-placeholder .el-icon {
  font-size: 16px;
  margin-bottom: 4px;
}

.image-placeholder span {
  font-size: 10px;
}

.card-description {
  font-size: 11px;
  color: #606266;
  line-height: 1.3;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  flex: 1;
}

.card-text {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
</style>
