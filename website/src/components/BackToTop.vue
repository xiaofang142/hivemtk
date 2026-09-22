<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const visible = ref(false)

function onScroll() {
  visible.value = window.scrollY > 400
}
function scrollToTop() {
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
onMounted(() => window.addEventListener('scroll', onScroll, { passive: true }))
onUnmounted(() => window.removeEventListener('scroll', onScroll))
</script>

<template>
  <transition name="btt-fade">
    <button v-show="visible" class="back-to-top" @click="scrollToTop" :aria-label="$t('回到顶部')">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <line x1="12" y1="19" x2="12" y2="5" />
        <polyline points="5 12 12 5 19 12" />
      </svg>
      <span class="btt-ring" aria-hidden="true"></span>
    </button>
  </transition>
</template>

<style scoped>
.back-to-top {
  position: fixed;
  right: 28px;
  bottom: 28px;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1px solid var(--primary-border);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--primary);
  color: #fff;
  box-shadow: var(--shadow-primary);
  z-index: 100;
  transition: transform 0.22s ease, background 0.22s ease, box-shadow 0.22s ease;
}

.back-to-top:hover {
  background: var(--primary-deep);
  transform: translateY(-3px);
  box-shadow: 0 12px 28px rgba(200, 57, 47, 0.4);
}

.back-to-top:active {
  transform: translateY(-1px);
}

.back-to-top svg {
  width: 20px;
  height: 20px;
  position: relative;
  z-index: 1;
}

/* 印章风内圈装饰 */
.btt-ring {
  position: absolute;
  inset: 5px;
  border: 1.5px solid rgba(255, 255, 255, 0.4);
  border-radius: 50%;
  pointer-events: none;
}

.btt-fade-enter-active,
.btt-fade-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.btt-fade-enter-from,
.btt-fade-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

@media (max-width: 480px) {
  .back-to-top {
    right: 20px;
    bottom: 20px;
    width: 44px;
    height: 44px;
  }
}
</style>
