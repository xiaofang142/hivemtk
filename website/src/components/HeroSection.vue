<script setup>
import { useSite } from '../composables/useSite.js'
const { hero } = useSite()
</script>

<template>
  <section class="hero" id="hero">
    <!-- 杂志顶栏:期号 + 日期 + 标语 -->
    <div class="hero-masthead">
      <div class="container masthead-inner">
        <span class="masthead-vol">VOL.01 · 2026</span>
        <span class="masthead-line"></span>
        <span class="masthead-tagline">{{ $t('私域 AI 营销操作系统 · 开源刊') }}</span>
        <span class="masthead-line"></span>
        <span class="masthead-edition">中 / EN / 日 / ع</span>
      </div>
    </div>

    <div class="container hero-body">
      <!-- 左:大字标题区 -->
      <div class="hero-content">
        <div class="eyebrow">
          <span
            v-for="tag in hero.eyebrow"
            :key="tag.text"
            class="tag"
            :class="tag.type === 'accent' ? 'tag-accent' : 'tag-primary'"
          >
            {{ tag.text }}
          </span>
        </div>

        <h1 class="hero-title">
          <template v-for="(line, index) in hero.title" :key="index">
            <span v-if="index === hero.gradientIndex" class="hero-title-accent">{{ line }}</span>
            <template v-else>{{ line }}</template>
          </template>
        </h1>

        <p class="hero-desc">{{ hero.description }}</p>

        <div class="hero-actions">
          <router-link :to="hero.primaryCta.href" class="btn btn-primary hero-cta-primary">
            <span>{{ hero.primaryCta.text }}</span>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
          </router-link>
          <a
            :href="hero.sourceCta.href"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-outline"
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
            <span>{{ hero.sourceCta.text }}</span>
          </a>
          <router-link :to="hero.secondaryCta.href" class="btn btn-ghost">
            {{ hero.secondaryCta.text }}
          </router-link>
        </div>
      </div>

      <!-- 右:数字封面卡 -->
      <aside class="hero-cover">
        <div class="cover-stamp">
          <span class="stamp-text">HiveMTK</span>
          <span class="stamp-sub">{{ $t('私域 AI 营销操作系统') }}</span>
        </div>

        <div class="cover-headline">
          <span class="cover-kicker">本期重点</span>
          <h2 class="cover-title">把销冠能力<br>复制给团队</h2>
        </div>

        <div class="cover-cards">
          <div
            v-for="(card, index) in hero.visualCards"
            :key="card.title"
            class="cover-card"
            :class="`cover-card-${index + 1}`"
          >
            <div class="cover-card-num">0{{ index + 1 }}</div>
            <div class="cover-card-icon" v-html="card.icon"></div>
            <div class="cover-card-text">
              <strong>{{ card.title }}</strong>
              <span>{{ card.desc }}</span>
            </div>
          </div>
        </div>

        <div class="cover-footer">
          <span>{{ $t('AGPL-3.0 · 开源 · 私有化') }}</span>
          <span class="cover-footer-dot"></span>
          <span>{{ $t('4 步部署') }}</span>
        </div>
      </aside>
    </div>

    <!-- 统计条:大数字,无"目标"前缀 -->
    <div class="hero-stats">
      <div class="container stats-grid">
        <div v-for="(stat, i) in hero.stats" :key="stat.label" class="stat-item">
          <div class="stat-num" :class="{ 'stat-num-accent': i === 0 || i === 1 }">{{ stat.value }}</div>
          <div class="stat-meta">
            <div class="stat-label">{{ stat.label }}</div>
            <div v-if="stat.sub" class="stat-sub">{{ stat.sub }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 三大差异化优势:引流入口的杀手锏 -->
    <div class="container hero-diff">
      <div class="diff-header">
        <span class="tag-line">{{ $t('为什么是 HiveMTK') }}</span>
        <h2 class="diff-title">{{ $t('不是又一个 SCRM,') }}<br>{{ $t('是') }}<span class="ink-mark">{{ $t('真正会卖货的 AI') }}</span></h2>
      </div>
      <div class="diff-grid">
        <article v-for="d in hero.differentiators" :key="d.num" class="diff-card">
          <div class="diff-num">{{ d.num }}</div>
          <h3 class="diff-card-title">{{ d.title }}</h3>
          <p class="diff-card-desc">{{ d.desc }}</p>
        </article>
      </div>
    </div>
  </section>
</template>

<style scoped>
.hero {
  position: relative;
  padding-top: 96px;
  background: var(--bg-base);
  overflow: hidden;
}

/* —— 杂志顶栏 —— */
.hero-masthead {
  border-top: 2px solid var(--text);
  border-bottom: 1px solid var(--border);
  padding: 12px 0;
  background: var(--bg-base);
}
.masthead-inner {
  display: flex;
  align-items: center;
  gap: 16px;
  font-family: var(--font-body);
  font-size: 0.74rem;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.masthead-vol {
  color: var(--primary);
}
.masthead-line {
  flex: 1;
  height: 1px;
  background: var(--border);
  max-width: 60px;
}
.masthead-tagline {
  color: var(--text);
  flex: 1;
  text-align: center;
}
.masthead-edition {
  color: var(--text-muted);
}

/* —— Hero 主体 —— */
.hero-body {
  display: grid;
  grid-template-columns: 1.15fr 0.85fr;
  gap: 72px;
  padding: 88px 32px 72px;
  align-items: start;
}

/* —— 左:内容区 —— */
.hero-content {
  max-width: 620px;
}

.eyebrow {
  display: flex;
  gap: 10px;
  margin-bottom: 28px;
  flex-wrap: wrap;
}

.hero-title {
  font-family: var(--font-display);
  font-size: clamp(2.8rem, 6.4vw, 5rem);
  font-weight: 900;
  line-height: 1.04;
  letter-spacing: -0.035em;
  margin-bottom: 28px;
  color: var(--text);
}

.hero-title-accent {
  color: var(--primary);
  font-style: italic;
  position: relative;
  display: inline-block;
}

.hero-desc {
  font-size: 1.12rem;
  color: var(--text-soft);
  margin-bottom: 36px;
  line-height: 1.78;
  max-width: 560px;
}

.hero-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 32px;
}

.hero-cta-primary {
  padding: 15px 28px;
  font-size: 1rem;
}

/* —— 右:封面卡 —— */
.hero-cover {
  position: relative;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 36px 32px;
  box-shadow: var(--shadow-md);
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.cover-stamp {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  text-align: right;
  padding-bottom: 16px;
  border-bottom: 2px solid var(--text);
}

.stamp-text {
  font-family: var(--font-display);
  font-size: 1.5rem;
  font-weight: 900;
  letter-spacing: -0.03em;
  color: var(--text);
  line-height: 1;
}

.stamp-sub {
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-muted);
  margin-top: 6px;
}

.cover-headline {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.cover-kicker {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--primary);
}

.cover-title {
  font-family: var(--font-display);
  font-size: 1.9rem;
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: -0.025em;
  color: var(--text);
}

.cover-cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.cover-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-base);
  transition: transform 0.2s ease, border-color 0.2s ease, background 0.2s ease;
  position: relative;
}
.cover-card:hover {
  transform: translateY(-2px);
  border-color: var(--primary);
  background: var(--bg-surface);
}

.cover-card-num {
  font-family: var(--font-mono);
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--text-dim);
  letter-spacing: 0.05em;
}

.cover-card-icon {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--primary);
  background: var(--primary-soft);
  border-radius: 7px;
}
.cover-card-icon :deep(svg) { width: 18px; height: 18px; }

.cover-card-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cover-card-text strong {
  font-family: var(--font-display);
  font-size: 0.92rem;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.01em;
}

.cover-card-text span {
  font-size: 0.74rem;
  color: var(--text-muted);
  line-height: 1.45;
}

.cover-footer {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 0.78rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--text-muted);
  padding-top: 16px;
  border-top: 1px solid var(--border);
}

.cover-footer-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--primary);
}

/* —— 统计条 —— */
.hero-stats {
  border-top: 2px solid var(--text);
  border-bottom: 1px solid var(--border);
  background: var(--bg-subtle);
  padding: 32px 0;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 32px;
}

.stat-item {
  display: flex;
  align-items: baseline;
  gap: 14px;
}

.stat-num {
  font-family: var(--font-display);
  font-size: clamp(2.8rem, 5vw, 3.8rem);
  font-weight: 900;
  line-height: 1;
  letter-spacing: -0.05em;
  color: var(--text);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.stat-num-accent {
  color: var(--primary);
}

.stat-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-bottom: 6px;
}

.stat-label {
  font-family: var(--font-display);
  font-size: 1rem;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.01em;
}

.stat-sub {
  font-size: 0.76rem;
  color: var(--text-muted);
  line-height: 1.4;
  max-width: 180px;
}

/* —— 三大差异化优势 —— */
.hero-diff {
  padding: 88px 32px;
}

.diff-header {
  max-width: 720px;
  margin-bottom: 48px;
}

.diff-title {
  font-family: var(--font-display);
  font-size: clamp(2rem, 4vw, 2.8rem);
  font-weight: 800;
  line-height: 1.15;
  letter-spacing: -0.025em;
  margin-top: 16px;
  color: var(--text);
}

.diff-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 24px;
}

.diff-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 32px;
  position: relative;
  transition: transform 0.22s ease, box-shadow 0.22s ease, border-color 0.22s ease;
}
.diff-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-lg);
  border-color: var(--primary);
}

.diff-num {
  font-family: var(--font-display);
  font-size: 3.2rem;
  font-weight: 900;
  line-height: 1;
  color: var(--primary);
  letter-spacing: -0.04em;
  margin-bottom: 18px;
  font-variant-numeric: tabular-nums;
}

.diff-card-title {
  font-family: var(--font-display);
  font-size: 1.22rem;
  font-weight: 800;
  line-height: 1.3;
  letter-spacing: -0.018em;
  color: var(--text);
  margin-bottom: 12px;
}

.diff-card-desc {
  font-size: 0.96rem;
  color: var(--text-muted);
  line-height: 1.75;
}

/* —— 响应式 —— */
@media (max-width: 992px) {
  .hero-body {
    grid-template-columns: 1fr;
    gap: 48px;
    padding: 64px 32px 56px;
  }
  .hero-content {
    max-width: 100%;
  }
  .hero-cover {
    order: -1;
    max-width: 480px;
  }
  .diff-grid {
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .stats-grid {
    grid-template-columns: repeat(2, 1fr);
    gap: 24px;
  }
}

@media (max-width: 640px) {
  .masthead-inner {
    font-size: 0.66rem;
    letter-spacing: 0.1em;
  }
  .masthead-tagline {
    display: none;
  }
  .hero-body {
    padding: 48px 20px 40px;
    gap: 36px;
  }
  .hero-actions {
    flex-direction: column;
    align-items: stretch;
  }
  .hero-actions .btn {
    justify-content: center;
  }
  .cover-cards {
    grid-template-columns: 1fr;
  }
  .stats-grid {
    grid-template-columns: 1fr;
    gap: 20px;
  }
  .stat-item {
    flex-direction: row;
    align-items: center;
    gap: 18px;
  }
  .stat-num {
    font-size: 2.6rem;
  }
  .hero-diff {
    padding: 56px 20px;
  }
  .diff-card {
    padding: 24px;
  }
  .diff-num {
    font-size: 2.4rem;
  }
}
</style>
