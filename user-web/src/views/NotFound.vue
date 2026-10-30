<template>
  <div class="not-found-container">
    <div class="error-content">
      <div class="error-code">
        {{ statusCode }}
      </div>
      <div class="error-message">
        <h1>{{ statusCode === 403 ? $t('无权访问') : $t('页面不存在') }}</h1>
        <p>{{ statusCode === 403 ? $t('您没有权限访问该页面') : '您访问的页面好像去了外太空...' }}</p>
        <el-button
          type="primary"
          @click="goBack"
        >
          <el-icon><ArrowLeft /></el-icon>
          {{ $t('返回上一页') }}
        </el-button>
        <el-button
          type="text"
          @click="goHome"
        >
          <el-icon><House /></el-icon>
          {{ $t('回到首页') }}
        </el-button>
      </div>
    </div>
    
    
    <div class="stars" />
    <div class="stars2" />
    <div class="stars3" />
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, House } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()

const statusCode = computed(() => (String(route.query.status || '') === '403' ? 403 : 404))

const goBack = () => {
  router.go(-1)
}

const goHome = () => {
  router.push('/');
}
</script>

<style scoped>
.not-found-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100vh;
  background-color: #1a1a2e;
  position: relative;
  overflow: hidden;
}

.error-content {
  text-align: center;
  color: white;
  z-index: 10;
}

.error-code {
  font-size: 150px;
  font-weight: bold;
  color: #e94560;
  margin-bottom: 20px;
  text-shadow: 0 0 30px rgba(233, 69, 96, 0.5);
  animation: pulse 3s infinite;
}

.error-message h1 {
  font-size: 36px;
  margin-bottom: 10px;
}

.error-message p {
  font-size: 18px;
  margin-bottom: 30px;
  color: #e2e2e2;
}

/* 星空背景效果 */
.stars, .stars2, .stars3 {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  width: 100%;
  height: 100%;
  display: block;
}

.stars {
  background: transparent url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='100' viewBox='0 0 100 100'%3E%3Ccircle cx='50' cy='50' r='1' fill='%23ffffff'/%3E%3C/svg%3E") repeat top center;
  z-index: 0;
  animation: moveStars 200s linear infinite;
}

.stars2 {
  background: transparent url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='100' viewBox='0 0 100 100'%3E%3Ccircle cx='25' cy='25' r='0.5' fill='%23ffffff'/%3E%3C/svg%3E") repeat top center;
  z-index: 0;
  animation: moveStars 150s linear infinite;
}

.stars3 {
  background: transparent url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='100' viewBox='0 0 100 100'%3E%3Ccircle cx='75' cy='75' r='0.3' fill='%23ffffff'/%3E%3C/svg%3E") repeat top center;
  z-index: 0;
  animation: moveStars 100s linear infinite;
}

/* 动画效果 */
@keyframes pulse {
  0%, 100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.05);
  }
}

@keyframes moveStars {
  from {
    background-position: 0 0;
  }
  to {
    background-position: 1000px 1000px;
  }
}
</style>  