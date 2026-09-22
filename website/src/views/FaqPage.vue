<script setup>
import { ref } from 'vue'
import { useSite } from '../composables/useSite.js'
const { faqSection } = useSite()

// 展开/收起状态(纯 UI 状态,首个 FAQ 默认展开方便阅读)
const openIndex = ref(0)

function toggle(idx) {
  openIndex.value = openIndex.value === idx ? -1 : idx
}
</script>

<template>
  <div class="faq-page">
    <!-- 编辑杂志风章节头 -->
    <section class="faq-hero">
      <div class="container faq-hero-inner">
        <div class="faq-masthead">
          <span class="fm-vol">{{ $t('FAQ · 问答') }}</span>
          <span class="fm-line"></span>
          <span class="fm-edition">{{ $t('HiveMTK 常见问题') }}</span>
        </div>

        <header class="faq-head">
          <span class="tag-line">{{ faqSection.tag }}</span>
          <h1 class="faq-title">
            <template v-for="(line, i) in faqSection.title" :key="i">
              <span v-if="i === 1" class="ink-mark">{{ line }}</span>
              <template v-else>{{ line }}</template>
            </template>
          </h1>
          <p class="faq-sub">{{ faqSection.subtitle }}</p>
        </header>

        <div class="faq-counter">
          <span class="counter-num">{{ String(faqSection.faqs.length).padStart(2, '0') }}</span>
          <span class="counter-label">{{ $t('个常见问题') }}</span>
        </div>
      </div>
    </section>

    <!-- 问答列表 -->
    <section class="faq-list-section">
      <div class="container faq-list">
        <article
          v-for="(item, idx) in faqSection.faqs"
          :key="idx"
          class="faq-item"
          :class="{ 'is-open': openIndex === idx }"
        >
          <button
            class="faq-q"
            type="button"
            :aria-expanded="openIndex === idx"
            @click="toggle(idx)"
          >
            <span class="q-num">{{ String(idx + 1).padStart(2, '0') }}</span>
            <span class="q-text">{{ item.q }}</span>
            <span class="q-chevron" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
            </span>
          </button>

          <div class="faq-a-wrap">
            <div class="faq-a-inner">
              <p class="faq-a">{{ item.a }}</p>
            </div>
          </div>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
.faq-page {
  background: var(--bg-base);
}

/* —— 顶部章节头 —— */
.faq-hero {
  border-top: 2px solid var(--text);
  border-bottom: 1px solid var(--border);
  background: var(--bg-base);
  padding: 64px 0 56px;
}

.faq-hero-inner {
  display: flex;
  flex-direction: column;
  gap: 28px;
}

/* 杂志顶栏 */
.faq-masthead {
  display: flex;
  align-items: center;
  gap: 14px;
  font-family: var(--font-body);
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.fm-vol { color: var(--primary); }
.fm-line {
  flex: 0 1 60px;
  height: 1px;
  background: var(--border-strong);
}

.faq-head {
  max-width: 760px;
}

.faq-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5.4vw, 3.4rem);
  font-weight: 900;
  line-height: 1.08;
  letter-spacing: -0.03em;
  margin: 16px 0 18px;
  color: var(--text);
}

.faq-sub {
  font-size: 1.08rem;
  color: var(--text-muted);
  line-height: 1.75;
  max-width: 680px;
}

/* 计数器 */
.faq-counter {
  display: inline-flex;
  align-items: baseline;
  gap: 8px;
  font-family: var(--font-mono);
  font-size: 0.78rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
  padding-top: 16px;
  border-top: 1px solid var(--border);
  align-self: flex-start;
}
.counter-num {
  font-family: var(--font-display);
  font-size: 1.5rem;
  font-weight: 900;
  color: var(--primary);
  letter-spacing: -0.02em;
}
.counter-label {
  font-weight: 600;
  letter-spacing: 0.12em;
}

/* —— 列表区 —— */
.faq-list-section {
  padding: 64px 0 96px;
}

.faq-list {
  max-width: 880px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* 单条 FAQ */
.faq-item {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  overflow: hidden;
  transition: border-color 0.22s ease, box-shadow 0.22s ease, transform 0.22s ease;
}
.faq-item:hover {
  border-color: var(--border-strong);
}
.faq-item.is-open {
  border-color: var(--primary);
  box-shadow: var(--shadow-md);
}

/* 问题按钮 */
.faq-q {
  display: flex;
  align-items: center;
  gap: 18px;
  width: 100%;
  padding: 22px 24px;
  text-align: left;
  background: none;
  border: none;
  cursor: pointer;
  font-family: inherit;
  color: inherit;
}

.q-num {
  font-family: var(--font-display);
  font-size: 1.6rem;
  font-weight: 900;
  color: var(--primary);
  letter-spacing: -0.04em;
  line-height: 1;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
  width: 38px;
}

.q-text {
  flex: 1;
  font-family: var(--font-display);
  font-size: 1.1rem;
  font-weight: 800;
  letter-spacing: -0.015em;
  line-height: 1.4;
  color: var(--text);
}

.q-chevron {
  width: 32px;
  height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-subtle);
  color: var(--text-muted);
  flex-shrink: 0;
  transition: transform 0.28s ease, background 0.22s ease, color 0.22s ease;
}
.q-chevron svg {
  width: 16px;
  height: 16px;
}
.faq-item.is-open .q-chevron {
  transform: rotate(180deg);
  background: var(--primary);
  color: #fff;
}

/* 答案:max-height 过渡 */
.faq-a-wrap {
  max-height: 0;
  overflow: hidden;
  transition: max-height 0.36s ease;
}
.faq-item.is-open .faq-a-wrap {
  max-height: 480px;
}

.faq-a-inner {
  padding: 0 24px 24px 80px;
  border-top: 1px dashed var(--border);
  margin-top: 0;
  padding-top: 18px;
}

.faq-a {
  margin: 0;
  color: var(--text-soft);
  font-size: 0.96rem;
  line-height: 1.78;
}

/* —— 响应式 —— */
@media (max-width: 768px) {
  .faq-hero {
    padding: 48px 0 40px;
  }
  .faq-list-section {
    padding: 48px 0 64px;
  }
  .faq-q {
    padding: 18px 18px;
    gap: 14px;
  }
  .q-num {
    font-size: 1.35rem;
    width: 32px;
  }
  .q-text {
    font-size: 1rem;
  }
  .q-chevron {
    width: 28px;
    height: 28px;
  }
  .faq-a-inner {
    padding: 16px 18px 20px 64px;
  }
}

@media (max-width: 480px) {
  .faq-q {
    gap: 10px;
    padding: 16px;
  }
  .q-num {
    font-size: 1.2rem;
    width: 28px;
  }
  .q-text {
    font-size: 0.94rem;
  }
  .faq-a-inner {
    padding: 14px 16px 18px 54px;
  }
  .faq-a {
    font-size: 0.9rem;
  }
}
</style>
