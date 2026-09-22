<script setup>
import { useSite } from '../composables/useSite.js'
const { workflowSection: section } = useSite()
</script>

<template>
  <section class="section workflow" id="workflow">
    <div class="container">
      <!-- 编辑杂志风章节头 -->
      <header class="workflow-head">
        <div class="head-meta">
          <span class="tag-line">{{ section.tag }}</span>
          <span class="head-counter">
            <span class="counter-num">{{ String(section.steps.length).padStart(2, '0') }}</span>
            <span class="counter-label">{{ $t('步业务流') }}</span>
          </span>
        </div>
        <h2 class="workflow-title">
          <template v-for="(line, index) in section.title" :key="index">
            <span v-if="index === section.gradientIndex" class="ink-mark">{{ line }}</span>
            <template v-else>{{ line }}</template>
            <br v-if="index < section.title.length - 1" />
          </template>
        </h2>
        <p class="workflow-subtitle">{{ section.subtitle }}</p>
      </header>

      <!-- 时间线:大数字步骤流 -->
      <div class="timeline">
        <div class="timeline-rail" aria-hidden="true">
          <span class="rail-line"></span>
          <span class="rail-cap"></span>
        </div>

        <article
          v-for="(step, index) in section.steps"
          :key="step.title"
          class="step"
        >
          <div class="step-num-block">
            <span class="step-kicker">STEP</span>
            <span class="step-num">{{ String(index + 1).padStart(2, '0') }}</span>
          </div>

          <div class="step-card">
            <div class="step-card-head">
              <span class="step-index">第 {{ index + 1 }} 步</span>
              <span class="step-divider"></span>
              <span class="step-total">共 {{ section.steps.length }} 步</span>
            </div>
            <h3 class="step-title">{{ step.title }}</h3>
            <p class="step-desc">{{ step.desc }}</p>
          </div>
        </article>

        <!-- 终点印章 -->
        <div class="timeline-end" aria-hidden="true">
          <span class="end-stamp">{{ $t('闭环') }}</span>
          <span class="end-text">{{ $t('回到第 1 步,持续迭代') }}</span>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.workflow {
  background: var(--bg-base);
  position: relative;
}

/* —— 章节头 —— */
.workflow-head {
  max-width: 880px;
  margin-bottom: 64px;
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

.workflow-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.4rem);
  font-weight: 900;
  line-height: 1.1;
  letter-spacing: -0.028em;
  margin-bottom: 20px;
  color: var(--text);
}

.workflow-subtitle {
  font-size: 1.08rem;
  color: var(--text-muted);
  line-height: 1.75;
  max-width: 720px;
}

/* —— 时间线 —— */
.timeline {
  position: relative;
  max-width: 920px;
  margin: 0 auto;
  padding-left: 12px;
}

.timeline-rail {
  position: absolute;
  left: 88px;
  top: 8px;
  bottom: 80px;
  width: 2px;
  pointer-events: none;
}
.rail-line {
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, var(--primary) 0%, var(--primary-border) 30%, var(--border-strong) 100%);
}
.rail-cap {
  position: absolute;
  bottom: -4px;
  left: 50%;
  transform: translateX(-50%);
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--primary);
  box-shadow: 0 0 0 4px var(--primary-soft);
}

/* 单步 */
.step {
  position: relative;
  display: grid;
  grid-template-columns: 76px 1fr;
  gap: 32px;
  margin-bottom: 28px;
  align-items: stretch;
}

.step:last-of-type {
  margin-bottom: 40px;
}

.step-num-block {
  position: relative;
  z-index: 2;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: flex-start;
  padding-top: 6px;
}

.step-kicker {
  font-family: var(--font-mono);
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--text-dim);
  margin-bottom: 2px;
}

.step-num {
  font-family: var(--font-display);
  font-size: clamp(2.4rem, 4vw, 3rem);
  font-weight: 900;
  line-height: 1;
  letter-spacing: -0.05em;
  color: var(--primary);
  font-variant-numeric: tabular-nums;
  background: var(--bg-base);
  padding: 4px 0;
}

/* 卡片 */
.step-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--primary);
  border-radius: var(--radius-lg);
  padding: 24px 28px;
  transition: transform 0.22s ease, box-shadow 0.22s ease, border-color 0.22s ease;
}
.step-card:hover {
  transform: translateX(4px);
  box-shadow: var(--shadow-lg);
  border-color: var(--primary);
  border-left-color: var(--primary-deep);
}

.step-card-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
  font-family: var(--font-mono);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.step-index {
  color: var(--primary);
  font-weight: 700;
}

.step-divider {
  flex: 0 1 32px;
  height: 1px;
  background: var(--border-strong);
}

.step-total {
  color: var(--text-dim);
}

.step-title {
  font-family: var(--font-display);
  font-size: 1.32rem;
  font-weight: 800;
  line-height: 1.25;
  letter-spacing: -0.018em;
  color: var(--text);
  margin-bottom: 10px;
}

.step-desc {
  color: var(--text-soft);
  font-size: 0.96rem;
  line-height: 1.78;
  margin: 0;
}

/* 终点印章 */
.timeline-end {
  position: relative;
  display: flex;
  align-items: center;
  gap: 14px;
  margin-left: 108px;
  padding: 18px 22px;
  background: var(--bg-subtle);
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-md);
}

.end-stamp {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 5px 12px;
  border-radius: 4px;
  background: var(--primary);
  color: #fff;
  font-family: var(--font-display);
  font-weight: 900;
  font-size: 0.84rem;
  letter-spacing: 0.06em;
  box-shadow: 0 3px 8px rgba(200, 57, 47, 0.25);
}

.end-text {
  font-size: 0.88rem;
  color: var(--text-muted);
  font-weight: 500;
}

/* —— 响应式 —— */
@media (max-width: 768px) {
  .timeline-rail {
    left: 32px;
  }
  .step {
    grid-template-columns: 56px 1fr;
    gap: 18px;
  }
  .step-num {
    font-size: 1.8rem;
  }
  .step-card {
    padding: 20px 20px;
  }
  .timeline-end {
    margin-left: 74px;
    padding: 14px 18px;
  }
}

@media (max-width: 480px) {
  .workflow-head {
    padding-top: 20px;
  }
  .timeline {
    padding-left: 0;
  }
  .timeline-rail {
    left: 24px;
  }
  .step {
    grid-template-columns: 44px 1fr;
    gap: 14px;
    margin-bottom: 20px;
  }
  .step-num {
    font-size: 1.5rem;
  }
  .step-card {
    padding: 16px;
  }
  .step-title {
    font-size: 1.1rem;
  }
  .step-desc {
    font-size: 0.9rem;
  }
  .timeline-end {
    margin-left: 58px;
    gap: 10px;
    padding: 12px 14px;
  }
  .end-stamp {
    font-size: 0.78rem;
    padding: 4px 10px;
  }
  .end-text {
    font-size: 0.82rem;
  }
}
</style>
