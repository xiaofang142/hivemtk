<script setup>
import { useSite } from '../composables/useSite.js'
const { toolchainSection: section } = useSite()
</script>

<template>
  <section class="section toolchain" id="toolchain">
    <div class="container">
      <!-- 编辑杂志风章节头 -->
      <header class="toolchain-head">
        <div class="head-meta">
          <span class="tag-line">{{ section.tag }}</span>
          <span class="head-counter">
            <span class="counter-num">42</span>
            <span class="counter-label">{{ $t('个智能体工具') }}</span>
          </span>
        </div>
        <h2 class="toolchain-title">
          <template v-for="(line, index) in section.title" :key="index">
            <span v-if="index === section.gradientIndex" class="ink-mark">{{ line }}</span>
            <template v-else>{{ line }}</template>
            <br v-if="index < section.title.length - 1" />
          </template>
        </h2>
        <p class="toolchain-subtitle">{{ section.subtitle }}</p>
      </header>

      <!-- 工程分层架构 -->
      <div class="architecture">
        <div class="architecture-head">
          <span class="arch-kicker">{{ section.architecture.title }}</span>
          <span class="arch-rule"></span>
        </div>
        <div class="layers">
          <div v-for="(layer, index) in section.architecture.layers" :key="layer.role" class="layer-card">
            <div class="layer-number">{{ String(index + 1).padStart(2, '0') }}</div>
            <div class="layer-role">{{ layer.role }}</div>
            <div class="layer-desc">{{ layer.desc }}</div>
          </div>
        </div>
      </div>

      <!-- 范例 + 代码块 -->
      <div class="example-grid">
        <div class="example-content">
          <span class="example-kicker">EXAMPLE</span>
          <h3 class="example-title">{{ section.example.title }}</h3>
          <p class="example-desc">{{ section.example.desc }}</p>
          <div class="guarantees">
            <div v-for="item in section.guarantees" :key="item.title" class="guarantee-item">
              <span class="guarantee-icon" v-html="item.icon" aria-hidden="true"></span>
              <div>
                <div class="guarantee-title">{{ item.title }}</div>
                <div class="guarantee-desc">{{ item.desc }}</div>
              </div>
            </div>
          </div>
        </div>

        <!-- 反白代码块:终端风 -->
        <div class="code-block">
          <div class="code-header">
            <span class="code-dots" aria-hidden="true">
              <span class="code-dot"></span>
              <span class="code-dot"></span>
              <span class="code-dot"></span>
            </span>
            <span class="code-label">AI Function Calling</span>
            <span class="code-prompt">$</span>
          </div>
          <pre><code>{{ section.example.code }}</code></pre>
        </div>
      </div>

      <!-- 工具分类卡片 -->
      <div class="categories">
        <article v-for="category in section.categories" :key="category.name" class="category-card">
          <div class="category-header">
            <div class="category-icon" v-html="category.icon" aria-hidden="true"></div>
            <div class="category-head-text">
              <h3 class="category-title">{{ category.title }}</h3>
              <span class="category-count">{{ category.count }} {{ $t('工具') }}</span>
            </div>
          </div>
          <p class="category-desc">{{ category.desc }}</p>
          <ul class="tool-list">
            <li v-for="tool in category.tools" :key="tool.name" class="tool-item">
              <code class="tool-name">{{ tool.name }}</code>
              <span class="tool-desc">{{ tool.desc }}</span>
            </li>
          </ul>
        </article>
      </div>
    </div>
  </section>
</template>

<style scoped>
.toolchain {
  background: var(--bg-base);
  position: relative;
}

/* —— 章节头 —— */
.toolchain-head {
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

.toolchain-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.4rem);
  font-weight: 900;
  line-height: 1.1;
  letter-spacing: -0.028em;
  margin-bottom: 20px;
  color: var(--text);
}

.toolchain-subtitle {
  font-size: 1.08rem;
  color: var(--text-muted);
  line-height: 1.75;
  max-width: 720px;
}

/* —— 工程分层架构 —— */
.architecture {
  margin-bottom: 72px;
}

.architecture-head {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 28px;
}

.arch-kicker {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--accent);
  flex-shrink: 0;
}

.arch-rule {
  flex: 1;
  height: 1px;
  background: var(--border-strong);
}

.layers {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.layer-card {
  position: relative;
  padding: 24px;
  border-radius: var(--radius-lg);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  transition: transform 0.22s ease, border-color 0.22s ease, box-shadow 0.22s ease;
  overflow: hidden;
}

.layer-card:hover {
  transform: translateY(-3px);
  border-color: var(--primary);
  box-shadow: var(--shadow-lg);
}

.layer-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: var(--primary);
  opacity: 0;
  transition: opacity 0.22s ease;
}
.layer-card:hover::before {
  opacity: 1;
}

.layer-number {
  font-family: var(--font-display);
  font-size: 2rem;
  font-weight: 900;
  color: var(--primary);
  line-height: 1;
  letter-spacing: -0.04em;
  margin-bottom: 14px;
  font-variant-numeric: tabular-nums;
}

.layer-role {
  font-family: var(--font-display);
  font-size: 1.05rem;
  font-weight: 800;
  letter-spacing: -0.015em;
  margin-bottom: 8px;
  color: var(--text);
}

.layer-desc {
  color: var(--text-muted);
  font-size: 0.88rem;
  line-height: 1.65;
}

/* —— 范例 + 代码块 —— */
.example-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 36px;
  align-items: stretch;
  margin-bottom: 80px;
}

.example-content {
  display: flex;
  flex-direction: column;
}

.example-kicker {
  display: inline-block;
  font-family: var(--font-mono);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--accent);
  margin-bottom: 12px;
}

.example-title {
  font-family: var(--font-display);
  font-size: 1.5rem;
  font-weight: 800;
  letter-spacing: -0.02em;
  margin-bottom: 14px;
  color: var(--text);
}

.example-desc {
  color: var(--text-muted);
  font-size: 1rem;
  line-height: 1.75;
  margin-bottom: 28px;
}

.guarantees {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 14px;
  margin-top: auto;
}

.guarantee-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  border-radius: var(--radius-md);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  transition: border-color 0.2s ease;
}
.guarantee-item:hover {
  border-color: var(--primary-border);
}

.guarantee-icon {
  display: inline-flex;
  color: var(--primary);
  background: var(--primary-soft);
  width: 36px;
  height: 36px;
  border-radius: var(--radius-sm);
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.guarantee-icon :deep(svg) { width: 20px; height: 20px; }

.guarantee-title {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: 0.96rem;
  margin-bottom: 4px;
  color: var(--text);
  letter-spacing: -0.01em;
}

.guarantee-desc {
  color: var(--text-muted);
  font-size: 0.84rem;
  line-height: 1.55;
}

/* —— 反白代码块:终端风 —— */
.code-block {
  border-radius: var(--radius-lg);
  background: var(--bg-ink);
  border: 1px solid var(--border-strong);
  overflow: hidden;
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
}

.code-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 18px;
  background: rgba(255, 255, 255, 0.04);
  border-bottom: 1px solid var(--border-inv);
}

.code-dots {
  display: inline-flex;
  gap: 6px;
}

.code-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: rgba(250, 247, 242, 0.2);
}
.code-dot:nth-child(1) { background: #FF6B5E; }
.code-dot:nth-child(2) { background: #F5C451; }
.code-dot:nth-child(3) { background: #67D6A8; }

.code-label {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--text-inv-muted);
  letter-spacing: 0.06em;
}

.code-prompt {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 0.9rem;
  font-weight: 700;
  color: #FF6B5E;
}

.code-block pre {
  padding: 22px;
  margin: 0;
  overflow-x: auto;
  flex: 1;
}

.code-block code {
  font-family: var(--font-mono);
  font-size: 0.84rem;
  line-height: 1.75;
  color: var(--text-inv);
  white-space: pre;
}

/* —— 工具分类卡片 —— */
.categories {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 24px;
}

.category-card {
  padding: 32px;
  border-radius: var(--radius-lg);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  transition: transform 0.22s ease, border-color 0.22s ease, box-shadow 0.22s ease;
}
.category-card:hover {
  transform: translateY(-3px);
  border-color: var(--primary);
  box-shadow: var(--shadow-lg);
}

.category-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}

.category-icon {
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--primary);
  background: var(--primary-soft);
  border-radius: var(--radius-md);
  flex-shrink: 0;
}
.category-icon :deep(svg) { width: 24px; height: 24px; }

.category-head-text {
  flex: 1;
  min-width: 0;
}

.category-title {
  font-family: var(--font-display);
  font-size: 1.25rem;
  font-weight: 800;
  letter-spacing: -0.018em;
  margin-bottom: 4px;
  color: var(--text);
}

.category-count {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  font-weight: 700;
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid var(--accent-border);
  padding: 2px 8px;
  border-radius: 4px;
  letter-spacing: 0.06em;
}

.category-desc {
  color: var(--text-muted);
  font-size: 0.94rem;
  line-height: 1.7;
  margin-bottom: 20px;
}

.tool-list {
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tool-item {
  display: flex;
  align-items: baseline;
  gap: 12px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--bg-subtle);
  border: 1px solid var(--border);
  transition: border-color 0.2s ease, background 0.2s ease;
}
.tool-item:hover {
  border-color: var(--primary-border);
  background: var(--bg-surface);
}

.tool-name {
  font-family: var(--font-mono);
  font-size: 0.8rem;
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid var(--accent-border);
  padding: 2px 8px;
  border-radius: 4px;
  flex-shrink: 0;
  font-weight: 600;
}

.tool-desc {
  color: var(--text-muted);
  font-size: 0.86rem;
  line-height: 1.5;
}

/* —— 响应式 —— */
@media (max-width: 1100px) {
  .layers {
    grid-template-columns: repeat(2, 1fr);
  }
  .example-grid,
  .categories {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .layers,
  .guarantees {
    grid-template-columns: 1fr;
  }
  .category-card {
    padding: 24px;
  }
  .example-grid {
    gap: 24px;
  }
}

@media (max-width: 480px) {
  .layer-card {
    padding: 18px;
  }
  .layer-number {
    font-size: 1.6rem;
  }
  .category-card {
    padding: 20px;
  }
  .category-header {
    gap: 12px;
  }
  .category-icon {
    width: 40px;
    height: 40px;
  }
}
</style>
