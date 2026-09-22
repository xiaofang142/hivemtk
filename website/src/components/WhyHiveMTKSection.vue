<script setup>
import { useSite } from '../composables/useSite.js'
const { whyHiveMTK } = useSite()
</script>

<template>
  <section class="why" id="why">
    <div class="container">
      <header class="why-head">
        <div class="head-meta">
          <span class="tag-line">{{ whyHiveMTK.tag }}</span>
          <span class="head-counter">
            <span class="counter-num">06</span>
            <span class="counter-label">{{ $t('大硬核理由') }}</span>
          </span>
        </div>
        <h2 class="why-title">
          <template v-for="(line, i) in whyHiveMTK.title" :key="i">
            <span v-if="i === whyHiveMTK.gradientIndex" class="ink-mark">{{ line }}</span>
            <template v-else>{{ line }}</template>
          </template>
        </h2>
        <p class="why-subtitle">{{ whyHiveMTK.subtitle }}</p>
      </header>

      <div class="pillars-grid">
        <article
          v-for="pillar in whyHiveMTK.pillars"
          :key="pillar.num"
          class="pillar-card"
          :id="`why-${pillar.key}`"
        >
          <div class="pillar-card-head">
            <div class="pillar-icon" v-html="pillar.icon"></div>
            <div class="pillar-key">{{ pillar.key }}</div>
            <div class="pillar-num">{{ pillar.num }}</div>
          </div>
          <h3 class="pillar-title">{{ pillar.title }}</h3>
          <p class="pillar-desc">{{ pillar.desc }}</p>
          <div class="pillar-metric">
            <span class="metric-value">{{ pillar.metric }}</span>
            <span class="metric-label">{{ pillar.metricLabel }}</span>
          </div>
        </article>
      </div>

      <div class="comparison" id="comparison">
        <h3 class="comparison-title">{{ whyHiveMTK.comparison.title }}</h3>
        <div class="comparison-table">
          <div class="comparison-row comparison-row-head">
            <div class="comp-cell comp-aspect">{{ $t('维度') }}</div>
            <div class="comp-cell comp-local">
              <span class="comp-badge">{{ $t('HiveMTK 本地部署') }}</span>
            </div>
            <div class="comp-cell comp-saas">
              <span class="comp-badge comp-badge-muted">{{ $t('传统 SaaS') }}</span>
            </div>
          </div>
          <div
            v-for="row in whyHiveMTK.comparison.rows"
            :key="row.aspect"
            class="comparison-row"
          >
            <div class="comp-cell comp-aspect">{{ row.aspect }}</div>
            <div class="comp-cell comp-local">
              <span v-if="row.localWin" class="win-mark" aria-hidden="true">✓</span>
              <span class="comp-text">{{ row.local }}</span>
            </div>
            <div class="comp-cell comp-saas">
              <span v-if="!row.localWin" class="win-mark" aria-hidden="true">✓</span>
              <span class="comp-text comp-text-muted">{{ row.saas }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.why {
  padding: var(--space-section) 0;
  background: var(--bg-subtle);
  position: relative;
}

.why-head {
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

.why-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.4rem);
  font-weight: 900;
  line-height: 1.1;
  letter-spacing: -0.028em;
  margin-bottom: 20px;
  color: var(--text);
}

.why-subtitle {
  font-size: 1.08rem;
  color: var(--text-muted);
  line-height: 1.75;
  max-width: 720px;
}

.pillars-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
  margin-bottom: 80px;
}

.pillar-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 28px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  transition: transform 0.22s ease, box-shadow 0.22s ease, border-color 0.22s ease;
  position: relative;
  scroll-margin-top: 100px;
}

.pillar-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-lg);
  border-color: var(--primary);
}

.pillar-card-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.pillar-icon {
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
.pillar-icon :deep(svg) {
  width: 22px;
  height: 22px;
}

.pillar-key {
  font-family: var(--font-display);
  font-size: 1.1rem;
  font-weight: 800;
  color: var(--text);
  letter-spacing: -0.01em;
  flex: 1;
}

.pillar-num {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--text-dim);
  letter-spacing: 0.05em;
}

.pillar-title {
  font-family: var(--font-display);
  font-size: 1.15rem;
  font-weight: 800;
  line-height: 1.3;
  letter-spacing: -0.018em;
  color: var(--text);
}

.pillar-desc {
  font-size: 0.92rem;
  color: var(--text-muted);
  line-height: 1.72;
  flex: 1;
}

.pillar-metric {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding-top: 14px;
  border-top: 1px dashed var(--border);
}

.metric-value {
  font-family: var(--font-display);
  font-size: 1.8rem;
  font-weight: 900;
  color: var(--primary);
  letter-spacing: -0.03em;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.metric-label {
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--text-muted);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.comparison {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 40px;
  box-shadow: var(--shadow-md);
}

.comparison-title {
  font-family: var(--font-display);
  font-size: clamp(1.5rem, 3vw, 2rem);
  font-weight: 800;
  line-height: 1.2;
  letter-spacing: -0.022em;
  margin-bottom: 28px;
  color: var(--text);
  text-align: center;
}

.comparison-table {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.comparison-row {
  display: grid;
  grid-template-columns: 1fr 1.5fr 1.5fr;
  gap: 0;
  border-bottom: 1px solid var(--border);
}

.comparison-row:last-child {
  border-bottom: none;
}

.comparison-row-head {
  border-bottom: 2px solid var(--text);
  padding-bottom: 14px;
  margin-bottom: 4px;
}

.comp-cell {
  padding: 16px 18px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.92rem;
  line-height: 1.55;
}

.comp-aspect {
  font-family: var(--font-display);
  font-weight: 700;
  color: var(--text);
  font-size: 0.96rem;
  letter-spacing: -0.01em;
}

.comp-local {
  background: var(--primary-soft);
  border-left: 3px solid var(--primary);
}

.comp-saas {
  background: var(--bg-subtle);
  border-left: 3px solid var(--border-strong);
}

.comp-badge {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  background: var(--primary);
  color: #fff;
}

.comp-badge-muted {
  background: var(--text-muted);
}

.comp-text {
  color: var(--text);
  font-weight: 500;
}

.comp-text-muted {
  color: var(--text-muted);
}

.win-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--success);
  color: #fff;
  font-size: 0.82rem;
  font-weight: 700;
  flex-shrink: 0;
}

@media (max-width: 992px) {
  .pillars-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .comparison {
    padding: 28px;
  }
}

@media (max-width: 640px) {
  .pillars-grid {
    grid-template-columns: 1fr;
  }
  .pillar-card {
    padding: 22px;
  }
  .comparison {
    padding: 20px;
  }
  .comparison-row {
    grid-template-columns: 1fr;
    gap: 0;
    padding: 14px 0;
    border-bottom: 1px solid var(--border);
  }
  .comparison-row-head {
    display: none;
  }
  .comp-cell {
    padding: 8px 12px;
  }
  .comp-aspect {
    font-size: 1.05rem;
    padding-top: 4px;
  }
}
</style>
