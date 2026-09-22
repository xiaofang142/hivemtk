<script setup>
import { featuresSection } from '../config/content.js'

defineProps({
  hideHeader: { type: Boolean, default: false },
})
</script>

<template>
  <section class="features" id="features">
    <div class="container">
      <!-- 章节头:编辑杂志式拉引语（在 /features 页面由 PageHeader 替代） -->
      <header v-if="!hideHeader" class="features-head">
        <div class="head-meta">
          <span class="tag-line">{{ featuresSection.tag }}</span>
          <span class="head-counter">
            <span class="counter-num">{{ String(featuresSection.features.length).padStart(2, '0') }}</span>
            <span class="counter-label">{{ $t('项核心能力') }}</span>
          </span>
        </div>
        <h2 class="features-title">
          <template v-for="(line, i) in featuresSection.title" :key="i">
            <span v-if="i === featuresSection.gradientIndex" class="ink-mark">{{ line }}</span>
            <template v-else>{{ line }}</template>
          </template>
        </h2>
        <p class="features-subtitle">{{ featuresSection.subtitle }}</p>
      </header>

      <!-- 功能卡片网格:可扫描结构 -->
      <div class="features-grid">
        <article
          v-for="(feature, index) in featuresSection.features"
          :key="feature.title"
          class="feature-card"
          :id="`feature-${feature.title.replace(/[\s()（）]/g, '-').toLowerCase()}`"
        >
          <div class="feature-card-head">
            <div class="feature-num">{{ String(index + 1).padStart(2, '0') }}</div>
            <div class="feature-icon" v-html="feature.icon"></div>
            <div class="feature-head-text">
              <h3 class="feature-title">{{ feature.title }}</h3>
              <div class="feature-industries">
                <span v-for="ind in feature.industries" :key="ind" class="industry-tag">{{ ind }}</span>
              </div>
            </div>
          </div>

          <div class="feature-body">
            <div class="feature-row feature-pain">
              <span class="row-label">{{ $t('痛点') }}</span>
              <p class="row-text">{{ feature.pain }}</p>
            </div>
            <div class="feature-row feature-solution">
              <span class="row-label">{{ $t('方案') }}</span>
              <p class="row-text">{{ feature.solution }}</p>
            </div>
            <div class="feature-row feature-special">
              <span class="row-label">{{ $t('亮点') }}</span>
              <p class="row-text">{{ feature.special }}</p>
            </div>
          </div>
        </article>
      </div>

      <!-- 章节尾:再次引流 -->
      <div class="features-foot">
        <p class="foot-text">{{ $t('以上仅为核心能力切片。94 业务模块 + 42 智能体工具,完整源码均在 GitHub / Gitee 公开。') }}</p>
        <div class="foot-repos">
          <a href="https://github.com/xiaofang142/hivemtk" target="_blank" rel="noopener noreferrer" class="foot-repo">
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-2c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.71 1.26 3.37.96.1-.75.4-1.26.74-1.55-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.04 0 0 .97-.31 3.18 1.18a11.05 11.05 0 0 1 5.79 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.58.24 2.75.12 3.04.74.81 1.18 1.84 1.18 3.1 0 4.43-2.7 5.41-5.27 5.69.41.36.78 1.07.78 2.16v3.2c0 .31.21.68.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5z"/></svg>
            <span>GitHub</span>
          </a>
          <a href="https://gitee.com/xhpmayun/hivemtk" target="_blank" rel="noopener noreferrer" class="foot-repo">
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M11.97 1.6c-5.7 0-10.32 4.62-10.32 10.32s4.62 10.32 10.32 10.32 10.32-4.62 10.32-10.32S17.67 1.6 11.97 1.6zm-.04 5.16c2.05 0 3.71 1.66 3.71 3.71 0 .55-.45 1-1 1H10.3v3.94c0 .55-.45 1-1 1s-1-.45-1-1V6.76c0-.55.45-1 1-1h2.63c.55 0 1 .45 1 1z"/></svg>
            <span>Gitee</span>
          </a>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.features {
  padding: var(--space-section) 0;
  background: var(--bg-base);
  position: relative;
}

/* —— 章节头 —— */
.features-head {
  max-width: 880px;
  margin-bottom: 56px;
  border-top: 2px solid var(--text);
  padding-top: 28px;
}

.head-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
  flex-wrap: wrap;
  gap: 12px;
}

.head-counter {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-family: var(--font-mono);
  color: var(--text-muted);
  font-size: 0.78rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.counter-num {
  font-family: var(--font-display);
  font-size: 1.4rem;
  font-weight: 900;
  color: var(--primary);
  letter-spacing: -0.02em;
}

.counter-label {
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

.features-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.4rem);
  font-weight: 900;
  line-height: 1.1;
  letter-spacing: -0.028em;
  margin-bottom: 20px;
  color: var(--text);
}

.features-subtitle {
  font-size: 1.08rem;
  color: var(--text-muted);
  line-height: 1.75;
  max-width: 720px;
}

/* —— 卡片网格 —— */
.features-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 20px;
}

.feature-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 28px 28px 24px;
  transition: transform 0.22s ease, box-shadow 0.22s ease, border-color 0.22s ease;
  scroll-margin-top: 100px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  position: relative;
}

.feature-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-lg);
  border-color: var(--primary);
}

/* —— 卡片头 —— */
.feature-card-head {
  display: flex;
  gap: 14px;
  align-items: flex-start;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}

.feature-num {
  font-family: var(--font-display);
  font-size: 1.65rem;
  font-weight: 900;
  color: var(--primary);
  letter-spacing: -0.04em;
  line-height: 1;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
  padding-top: 4px;
}

.feature-icon {
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--primary-soft);
  color: var(--primary);
  border-radius: 10px;
  flex-shrink: 0;
}
.feature-icon :deep(svg) {
  width: 22px;
  height: 22px;
}

.feature-head-text {
  flex: 1;
  min-width: 0;
}

.feature-title {
  font-family: var(--font-display);
  font-size: 1.3rem;
  font-weight: 800;
  line-height: 1.25;
  letter-spacing: -0.018em;
  color: var(--text);
  margin-bottom: 8px;
}

.feature-industries {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.industry-tag {
  display: inline-flex;
  padding: 3px 9px;
  border-radius: 4px;
  font-size: 0.7rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  background: var(--bg-subtle);
  color: var(--text-muted);
  border: 1px solid var(--border);
}

/* —— 卡片正文:痛点 / 方案 / 亮点 —— */
.feature-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.feature-row {
  display: grid;
  grid-template-columns: 36px 1fr;
  gap: 12px;
  align-items: start;
}

.row-label {
  font-family: var(--font-display);
  font-size: 0.78rem;
  font-weight: 800;
  letter-spacing: 0.06em;
  text-align: center;
  padding: 3px 0;
  border-radius: 4px;
  background: var(--bg-subtle);
  color: var(--text);
  height: fit-content;
  line-height: 1.4;
}

.feature-pain .row-label {
  background: rgba(180, 50, 50, 0.08);
  color: #B43232;
}

.feature-solution .row-label {
  background: var(--accent-soft);
  color: var(--accent);
}

.feature-special .row-label {
  background: var(--primary);
  color: #fff;
}

.row-text {
  font-size: 0.92rem;
  color: var(--text-soft);
  line-height: 1.72;
  margin: 0;
}

.feature-special .row-text {
  color: var(--text);
  font-weight: 500;
}

/* —— 章节尾 —— */
.features-foot {
  margin-top: 48px;
  padding: 32px;
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  flex-wrap: wrap;
}

.foot-text {
  font-family: var(--font-display);
  font-size: 1.08rem;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.01em;
  flex: 1;
  min-width: 260px;
}

.foot-repos {
  display: flex;
  gap: 10px;
  flex-shrink: 0;
}

.foot-repo {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 18px;
  border-radius: var(--radius-md);
  background: var(--bg-surface);
  border: 1px solid var(--border-strong);
  color: var(--text);
  font-size: 0.9rem;
  font-weight: 600;
  transition: all 0.18s ease;
}

.foot-repo svg {
  width: 18px;
  height: 18px;
}

.foot-repo:hover {
  background: var(--primary);
  color: #fff;
  border-color: var(--primary);
  transform: translateY(-1px);
}

/* —— 响应式 —— */
@media (max-width: 860px) {
  .features-grid {
    grid-template-columns: 1fr;
  }
  .feature-card {
    padding: 24px;
  }
  .features-foot {
    padding: 24px;
    flex-direction: column;
    align-items: stretch;
    text-align: center;
  }
  .foot-repos {
    justify-content: center;
  }
}

@media (max-width: 480px) {
  .feature-card-head {
    flex-wrap: wrap;
  }
  .feature-title {
    font-size: 1.15rem;
  }
  .feature-row {
    grid-template-columns: 28px 1fr;
    gap: 10px;
  }
  .row-label {
    font-size: 0.7rem;
  }
  .row-text {
    font-size: 0.88rem;
  }
}
</style>
