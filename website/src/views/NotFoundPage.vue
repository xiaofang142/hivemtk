<template>
  <section class="not-found">
    <div class="container nf-inner">
      <!-- 杂志顶栏:期号 + 期号横线 -->
      <div class="nf-masthead">
        <span class="nf-vol">ERR · 404</span>
        <span class="nf-line"></span>
        <span class="nf-edition">{{ $t('页面未收录') }}</span>
      </div>

      <!-- 超大数字 404 -->
      <div class="nf-code" aria-hidden="true">
        <span class="nf-digit">4</span>
        <span class="nf-digit nf-digit-zero">0</span>
        <span class="nf-digit">4</span>
      </div>

      <!-- 印章 -->
      <div class="nf-stamp" aria-hidden="true">
        <span>NOT<br />FOUND</span>
      </div>

      <h1 class="nf-title">{{ $t('页面未找到') }}</h1>
      <p class="nf-desc">
        {{ $t('您访问的页面不存在或已迁移，请尝试返回首页或查看安装文档。') }}
      </p>

      <div class="nf-actions">
        <router-link to="/" class="btn btn-primary nf-btn-primary">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>
          <span>{{ $t('返回首页') }}</span>
        </router-link>
        <router-link to="/docs" class="btn btn-ghost">{{ $t('查看文档') }}</router-link>
        <router-link to="/deploy" class="btn btn-ghost">{{ $t('部署指南') }}</router-link>
      </div>

      <!-- 页脚横线 + 标语 -->
      <div class="nf-foot">
        <span class="nf-foot-line"></span>
        <span class="nf-foot-text">HiveMTK · {{ $t('私域 AI 营销操作系统') }}</span>
        <span class="nf-foot-line"></span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.not-found {
  min-height: 78vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 96px 0;
  background: var(--bg-base);
}

.nf-inner {
  text-align: center;
  max-width: 640px;
  margin: 0 auto;
  position: relative;
}

/* —— 杂志顶栏 —— */
.nf-masthead {
  display: flex;
  align-items: center;
  gap: 16px;
  justify-content: center;
  font-family: var(--font-body);
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--text-muted);
  margin-bottom: 32px;
}

.nf-vol {
  color: var(--primary);
}

.nf-line {
  flex: 0 1 60px;
  height: 1px;
  background: var(--border-strong);
}

/* —— 超大数字 404 —— */
.nf-code {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-bottom: 28px;
  line-height: 1;
}

.nf-digit {
  font-family: var(--font-display);
  font-size: clamp(7rem, 22vw, 11rem);
  font-weight: 900;
  letter-spacing: -0.06em;
  line-height: 0.9;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.nf-digit-zero {
  color: var(--primary);
  font-style: italic;
  position: relative;
}

/* 中间 0 上叠加印章 */
.nf-digit-zero::after {
  content: '';
  position: absolute;
  inset: -8% -6%;
  border: 2px dashed var(--primary-border);
  border-radius: 50%;
  pointer-events: none;
}

/* —— 印章戳记 —— */
.nf-stamp {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 76px;
  height: 76px;
  border-radius: 50%;
  background: var(--primary);
  color: #fff;
  font-family: var(--font-display);
  font-weight: 900;
  font-size: 0.92rem;
  line-height: 1.05;
  letter-spacing: 0.04em;
  text-align: center;
  box-shadow: 0 6px 18px rgba(200, 57, 47, 0.3);
  transform: rotate(-12deg);
  margin-bottom: 28px;
  position: relative;
}
.nf-stamp::after {
  content: '';
  position: absolute;
  inset: 5px;
  border: 1.5px solid rgba(255, 255, 255, 0.42);
  border-radius: 50%;
}

.nf-title {
  font-family: var(--font-display);
  font-size: clamp(1.6rem, 3vw, 2rem);
  font-weight: 800;
  letter-spacing: -0.025em;
  margin-bottom: 14px;
  color: var(--text);
}

.nf-desc {
  font-size: 1rem;
  color: var(--text-muted);
  line-height: 1.75;
  margin: 0 auto 36px;
  max-width: 460px;
}

.nf-actions {
  display: flex;
  gap: 12px;
  justify-content: center;
  flex-wrap: wrap;
  margin-bottom: 56px;
}

.nf-btn-primary svg {
  flex-shrink: 0;
}

/* —— 页脚横线 —— */
.nf-foot {
  display: flex;
  align-items: center;
  gap: 14px;
  justify-content: center;
  font-family: var(--font-body);
  font-size: 0.74rem;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.nf-foot-line {
  flex: 0 1 80px;
  height: 1px;
  background: var(--border-strong);
}

@media (max-width: 480px) {
  .nf-actions {
    flex-direction: column;
    align-items: stretch;
  }
  .nf-actions .btn {
    justify-content: center;
  }
  .nf-masthead {
    font-size: 0.66rem;
    letter-spacing: 0.12em;
  }
  .nf-stamp {
    width: 64px;
    height: 64px;
    font-size: 0.78rem;
  }
}
</style>
